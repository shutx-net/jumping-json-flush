# SVG ER diagram generation — design investigation

**Status: investigation, nothing implemented.** This document records what a
self-contained SVG exporter would have to do, which algorithm to pick at each
step and why, and what it costs. It is written before any code because the
layout choices are the kind that are expensive to revisit once output has
shipped — the same reason `design/ddl-export.md` exists.

## Why

`jjf export dot` writes DOT source and stops there. The `.dot` is not a
diagram; it is instructions for a program the reader has to install. That is
the only place in jjf where the artifact is not the thing the user wanted:

```
db-design.json --[jjf]--> er.dot --[your graphviz]--> er.svg
```

`AGENTS.md` says the binary avoids CGO and external runtime requirements, and
`internal/schema` and `internal/sml` already show what the project is willing
to pay for that: a JSON Schema evaluator and an XLSX writer, both written here,
both to keep `go.mod` empty. Graphviz is the last external requirement standing,
and it is the one a reader hits rather than a maintainer.

`jjf export svg` would close it. One command, no install, an image.

## What graphviz is actually doing today

The honest inventory, because this is the size of the hole to fill. For each
`.dot` file jjf writes, `dot` currently performs:

1. **Label layout** — measures every string in every HTML-like `<TABLE>` cell
   against a real font, sizes the cells, sizes the box.
2. **Cycle removal** — an ER schema has mutual and self references; layering
   needs a DAG.
3. **Layer assignment** — network simplex over the ranking problem.
4. **Crossing reduction** — iterated median/transpose over the layer orders.
5. **Coordinate assignment** — priority or Brandes–Köpf within layers.
6. **Edge routing** — piecewise Bezier splines through the free space.
7. **Arrowhead composition** — `crowodot`, `crowtee`, `teeodot`, `teetee`
   drawn at the right end, at the right angle, in the right order.
8. **SVG serialisation.**

Items 2–5 are the classical Sugiyama framework and are the interesting part of
this document. Item 1 is the one with no good answer in a language with no font
engine and no permitted dependency, and it is discussed at length below because
it is the most likely thing to be got wrong. Items 6–8 are mechanical but full
of small traps that only appear in a real renderer.

## Layout: which family

| Family | What it gives | Why not / why |
|---|---|---|
| **Sugiyama layered** (dot, dagre, ELK layered) | Layers, short edges, few crossings, a natural left-to-right reading order for "child references parent" | **Chosen.** It is what the current `rankdir=LR` output already is, so switching renderers does not also change what the diagram means. It decomposes into five independent, individually testable phases, each of which has a cheap variant and an expensive upgrade |
| **Force-directed** (Fruchterman–Reingold, Kamada–Kawai) | ~200 lines for a first version | Rejected. It treats nodes as points, and ER nodes are large rectangles of wildly different heights, so it needs an overlap-removal pass bolted on. It produces straight edges that cross boxes, so routing is unsolved. And it wants transcendental functions in its inner loop, which is the one class of float operation Go does not promise is identical across architectures — determinism is a stated property of every jjf exporter |
| **Orthogonal / topology-shape-metrics** | The prettiest ER diagrams in the literature | Rejected. Planarisation, orthogonal representation, compaction: it is a textbook's worth of code, several times the size of the rest of jjf |
| **Grid packing** | ~150 lines, fully deterministic | Rejected as the destination, viable as a stepping stone. Row-major boxes with a channel router is implementable in a day and looks like it. Not worth shipping as output people are told to regenerate |

The precedent worth leaning on is **dagre**, the JavaScript layered layout that
Mermaid uses to draw ER diagrams in SVG without graphviz. It follows Gansner et
al., *A Technique for Drawing Directed Graphs*, which is also what `dot` itself
implements — so "layered, in our own code, to SVG, for ER diagrams" is a path
that has been walked.

## Layout: which algorithm per phase

The rule applied throughout: **take the cheap variant, and write down the
upgrade.** Each phase is replaceable behind the phase boundary, so a bad-looking
diagram is a localised fix rather than a rewrite.

### Phase 0 — building the layout graph

Nodes are the document's tables in document order, then the stub nodes for
undefined foreign key targets in first-reference order — the same two lists
`internal/export/dot` already builds, and for the same reason: the order is the
determinism.

Edges are one per foreign key, child → parent, matching the existing edge
statements exactly. Two adjustments before layering:

* **Self-loops** (`categories` → `categories` in the `edge.json` fixture) are
  removed from the layout graph and drawn separately. Every layered algorithm
  assumes they are gone.
* **Parallel edges** (`edge` → `nodes` twice in the same fixture) collapse to
  one layout edge with a multiplicity. They occupy the same channel; the extra
  copies only need distinct lanes and distinct ports at routing time, which is
  a routing concern, not a layering one.

### Phase 1 — cycle removal

**Choice: DFS back-edge reversal, visiting vertices in document order.**

The alternative is a greedy feedback arc set (Eades–Lin–Smyth), which reverses
provably fewer edges. It is not worth 50 lines here, and the reason is specific
to ER diagrams rather than general: **the direction of a drawn relationship is
carried by its crow's foot markers, not by its geometry.** A reversed edge is
reversed only for the ranking arithmetic; it is drawn with the same marks at
the same ends, so the reader cannot tell which edges were reversed. In a
flowchart, cycle-breaking quality is visible; here it only shifts a box a layer.

### Phase 2 — layer assignment

**Choice: longest-path ranking, then one tightening pass. Then multiply every
rank by two.**

Longest-path is `rank(v) = 0` for a source, else `max(rank(u)+1)` over
predecessors — twenty lines, minimum number of layers, and it bunches nodes
towards the sources. The tightening pass moves each node to the latest rank its
successors permit when that shortens more edges than it lengthens, which
recovers most of what network simplex would buy.

**Network simplex** (Gansner et al., what `dot` and dagre use by default) is
the upgrade: it minimises total weighted edge length exactly. It needs a
feasible tight tree, cut values, and a leave/enter edge search — 250 to 350
lines. Do not start there. Reach for it if, and only if, real documents come out
looking stretched.

The **doubling** is a trick worth taking from `dot` on day one rather than
retrofitting: after ranking, replace every rank `r` with `2r`. Every edge then
spans at least two layers, so every edge has an intermediate layer with a free
slot in it, and **edge labels become dummy nodes** placed in that slot by the
same crossing reduction and coordinate assignment that place everything else.
The alternative — routing first and then hunting for somewhere to put the
labels — is the thing that makes hand-rolled diagram generators look hand-rolled.
Every relationship in the current DOT output carries a label, so this is not an
optional refinement.

### Phase 3 — dummy nodes

Every edge spanning more than one layer becomes a chain of dummy nodes, one per
intermediate layer. Dummies are zero-height and carry a reserved lane width.
Mechanical, ~50 lines, but it is what makes phases 4 and 5 able to treat long
edges as ordinary nodes and so keep them straight.

### Phase 4 — crossing reduction

**Choice: median heuristic, fixed sweep count, plus transpose; keep the best
ordering seen.**

* Initial order: DFS from the first table in document order.
* Sweep down and up a fixed number of times — single digits is the conventional
  range — reordering each layer by the median of its neighbours' positions in
  the fixed layer.
* After each sweep, `transpose`: swap adjacent pairs while the swap reduces
  crossings.
* Count crossings after each round and keep the best. **A fixed iteration count,
  not a convergence test**, so the result can never depend on how the loop
  happened to terminate.

Median with transpose is what Gansner et al. specify and what `dot` runs, so
choosing it keeps the new renderer's output close to the output people already
have. Barycenter is the near-equivalent alternative and is what several
implementations use instead; Jünger and Mutzel's survey of bilayer heuristics is
the place to settle the question if the drawings ever justify revisiting it.

Two determinism obligations that are easy to miss and fatal to golden files:

* The median tie-break must be **total**: median value, then current position,
  then node index. Anything less and two nodes can swap on a rebuild.
* Crossing counting can be the naive O(k²) double loop. The
  Barth–Jünger–Mutzel accumulator-tree counter is O(|E| log |V|) and pointless
  at a scale of tens of tables — fifteen lines against a hundred, for a
  difference no one can measure.

### Phase 5 — coordinate assignment

With `rankdir=LR`, layer index gives X and position within a layer gives Y.

* **X** is a running sum: layer width is the widest box in that layer, plus a
  fixed inter-layer gap, plus whatever the label layer between them needs.
* **Y** is the interesting one. **Choice: the priority method.** Nodes are
  nudged toward the median of their neighbours in priority order, with dummy
  nodes given the highest priority so that long edges come out straight. About
  120 lines.

**Brandes–Köpf** is the upgrade, and the reason to defer it is concrete rather
than a matter of taste: the algorithm as published contains two flaws, one of
which was not documented anywhere until the 2020 erratum, and which "requires a
non-trivial adaptation" to fix. Implementing it correctly means implementing
the erratum too. That is not a first version.

### Phase 6 — edge routing

**Choice: orthogonal polylines through the dummy-node positions.**

* A forward edge leaves the right side of the child box and enters the left
  side of the parent; an edge reversed in phase 1 runs the other way and
  attaches to the opposite sides. Both are known at emission time, so the
  attachment point and the outgoing direction are always exactly known and
  always axis-aligned — which is what makes the markers below drawable as plain
  paths.
* **Lanes**: each edge gets a distinct X within the inter-layer channel,
  assigned in order of target Y, so two edges crossing the same channel never
  share a segment. This is where collapsed parallel edges get separated again.
* **Ports**: multiple edges on one side of a box get distinct offsets along
  that side, ordered by the Y of the far end, so they fan out instead of
  converging on one point.
* **Self-loops**: a fixed shape — out of the right side, around, back into the
  right side — one rung further out per additional loop on that table.
* Corners: square. Rounded corners need arcs, and an `A` command with an
  integer radius stays integer, so this can be added later without disturbing
  anything else.

Splines are not on the list. Bezier fitting through a corridor is a project of
its own, and a clean orthogonal ER diagram is a perfectly conventional-looking
ER diagram.

**Deliberately deferred: per-column ports.** Because jjf owns the layout, it
*could* attach each relationship at the Y of its actual foreign key column row
rather than at the box edge — strictly more information than the current DOT
output, which uses no ports at all. It is also the point at which port ordering
becomes a constraint inside crossing reduction (the "generalized port
constraints" problem), so it belongs after the basic layout works, not in it.

## Text measurement: the actual hard part

Everything above is arithmetic on numbers that are only correct if the box
sizes are correct, and box sizes come from text widths. Go's standard library
has no font engine, and `go.mod` has no requires and nothing may add one. So
the widths must be **modelled**.

**The governing principle: the model must be an upper bound, never a best
estimate.** A layout computed from an upper bound has too much whitespace on
some machines. A layout computed from an average has text spilling out of its
box on some machines. Only one of those is a bug.

Three facts make this tractable rather than hopeless:

1. **The schema forces most of the text to be ASCII.** `$defs/identifier` is
   `^[A-Za-z_][A-Za-z0-9_]*$` and `$defs/columnType` is
   `^[A-Za-z][A-Za-z0-9_ ]*$`. Every physical name and every rendered type in
   the diagram is ASCII by construction. Only `logicalName` is free text.
2. **CJK is exact.** East Asian wide glyphs are one em, in every font, by
   definition of the typography. The fixtures' Japanese logical names are the
   part of the measurement that cannot be wrong.
3. **A monospace font is nearly exact.** Common monospace faces sit between
   0.55 em (Consolas) and 0.60 em (DejaVu Sans Mono, Liberation Mono, Menlo,
   Courier New) per character.

Which gives the recommendation: **two font roles.**

* **Monospace** for physical names and types. Reserve at the upper bound of the
  range — 0.60 em, or 0.62 em for headroom — times the character count. Exact
  arithmetic, and over-reserving on a narrower face costs only whitespace.
* **Sans** for logical names and the table header. Per-rune widths: a 95-entry
  table of ASCII advances taken from the Liberation Sans / Arial / Helvetica
  metric family, 1.0 em for East Asian wide and fullwidth, 0.5 em for halfwidth
  katakana, and 1.0 em as the fallback for anything unrecognised — conservative
  by construction.

**A gap to budget for: Go's `unicode` package has no East Asian Width
property.** `unicode.Han`, `Hiragana` and `Katakana` exist, but the W and F
classes also cover CJK symbols and punctuation, fullwidth forms, and more, and
they are script-Common. `golang.org/x/text/width` has the table and is a
dependency, so it is out. A hand-rolled range table of roughly 25 ranges is
required. That is a small, self-contained, testable file — but it is a file
nobody expects to write, and it is the sort of thing that turns a two-week
estimate into three.

Rejected alternatives, with reasons:

* **Embed a subsetted font as a data URI.** This is the only approach that is
  exactly right rather than bounded. It needs a TrueType parser and writer to
  subset, because an unsubsetted CJK font is several megabytes, and the diagram
  would carry a font binary in the repository. Out of proportion.
* **Convert text to paths.** Same font parser, plus a glyph outline
  interpreter, and it destroys selectable and searchable text.
* **`textLength` with `lengthAdjust`.** Pins each string to the width the model
  predicted, so it cannot overflow whatever font the reader has. Rejected:
  `spacing` inserts gaps between CJK glyphs, which looks broken, and
  `spacingAndGlyphs` distorts the letterforms. Support outside browsers is
  uneven.
* **`<foreignObject>` with HTML.** Requires an HTML layout engine in the
  viewer. Inkscape and librsvg do not have one; the file would render blank
  outside a browser.

The remaining exposure is a substituted proportional font wider than the model
in a logical name. The belt-and-braces answer is a per-cell `clipPath` plus a
`<title>` carrying the untruncated string. That costs generated ids, which
brings its own caveat (below), so it is a defensible thing to leave out of a
first version and add if anyone reports clipping.

## SVG emission

These are not layout questions, but each one is a way to produce a file that is
correct and still looks wrong somewhere.

| # | Choice | Decision | Why |
|---|---|---|---|
| 1 | Arrowheads | **Explicit `<path>` elements. No `<marker>`.** | We already know the exact attachment point and the exact outgoing direction, so a marker's whole value — `orient="auto"` — is redundant. And a marker at the *start* of a path points the wrong way unless `orient="auto-start-reverse"` is used, which is SVG 2 and which librsvg does not implement, so a crow's foot would silently face backwards in some renderers. Explicit paths are four little path strings and they are right everywhere |
| 2 | Vertical text centring | **Compute the baseline from font size. Never `dominant-baseline`.** | GitHub's SVG sanitizer strips `dominant-baseline` from `<text>` nodes (github/markup#1160), so a diagram centred that way is correct in a browser and misaligned in the README it was made for. `text-anchor` survives sanitisation and can be used for horizontal alignment |
| 3 | Styling | **Presentation attributes, not `class` or `style`.** | The same sanitiser strips class and inline style. A `<style>` block does survive when the SVG is referenced with `<img src>`, but relying on the difference between the two embedding paths is a trap. Attributes work in both, at the cost of a larger file |
| 4 | Background | **An explicit opaque `<rect>` covering the viewBox.** | An SVG with no background is transparent, and a transparent diagram of dark text is unreadable on a dark-mode README — the single most likely place this file gets looked at |
| 5 | Dark mode | **Out of scope.** | It would need `@media (prefers-color-scheme)` inside a `<style>` block, which is exactly what choice 3 says cannot be relied on |
| 6 | Sizing | **`viewBox="0 0 W H"` plus `width` and `height` in px.** | The viewBox makes it scale; the intrinsic size stops it collapsing or filling its container in the places that need one |
| 7 | XML validity | **Sanitise C0 control characters before escaping.** | `logicalName` and `description` have `maxLength` but no `pattern`, so a document may legally carry U+0001 in one. XML 1.0 has no escape for C0 controls other than tab, LF and CR — such a character makes the file malformed, not merely ugly. **This is a failure mode the DOT exporter does not have**, and the one place the SVG writer must do something the DOT writer does not. Replace with U+FFFD. Invalid UTF-8 is not a concern: `encoding/json` has already substituted it |
| 8 | Escaping | `&`, `<`, `>` in text; those plus `"` in attribute values | The shape of `escapeHTML` in `internal/export/dot/text.go`, which is one more argument for the shared package below |
| 9 | Generated ids | **Sequential, `jjf-` prefixed, in emission order.** Never derived from a name | Names repeat and can hold characters an XML id cannot. Note the caveat: two jjf diagrams inlined into one HTML page share an id namespace and would collide. `<img src>` isolates them. If clipping is skipped, no ids are generated at all and the question disappears |
| 10 | Header | The same two-line comment the DOT and DDL exporters write | Consistency, and it is where a reader decides whether to commit the file |

## Determinism

`jjf` promises byte-identical output for identical input, and a layout engine is
the first thing in the project with a real opportunity to break it.

* **Integers everywhere.** All coordinates in integer user units; text metrics
  in integer thousandths of an em, rounded up at the point a box is sized. No
  float reaches the output, so there is no question of float formatting, and no
  question of whether `math` behaves identically on two architectures.
* **No map is ever ranged over.** The existing rule in
  `internal/export/dot/export.go` — maps are looked up, slices are walked —
  extends unchanged, and matters much more here: a map range inside crossing
  reduction would produce a different but equally valid drawing on every run.
* **Fixed iteration counts** in every heuristic loop, and **total tie-break
  orders** in every comparison.

## Where the code goes

```
internal/erd/            shared derivation: the diagram model, cardinality
internal/export/svg/     metrics, layout, routing, theme, writer
```

`internal/erd` exists to hold what `internal/export/dot` has already worked out
and what an SVG exporter must not work out differently:
`cardinality.go` in full (`end`, `childEnd`, `uniqueIn`, `sameColumnSet`,
`findColumn`), plus `markerOf`, `renderType`, `definedTables`,
`undefinedTargets` and `edgeLabel`.

The alternative is to copy it, and copying is how the two exporters come to
disagree about whether one particular relationship is optional — a difference
no golden file would flag, because each file would be individually correct.
`renderType` already carries a comment promising it mirrors `sizeOf` in
`internal/export/xlsx/tabledef.go`; that is two copies of one rule maintained by
comment, and a third would be worse. The extraction is a mechanical, no-output-
change refactor that leaves the dot goldens byte-identical, and it is worth
doing on its own regardless of whether the SVG exporter is ever written.

Everything else stays inside `internal/export/svg` as separate files rather
than separate packages. `AGENTS.md` warns against packages created to match a
layout diagram, and layout, routing and serialisation have exactly one consumer
each.

Wiring is one entry in `exportFormats()` in `cmd/jjf/export.go`: `name: "svg"`,
`ext: ".svg"`, `binary: false` — SVG is text and may go to a terminal — and no
`accept`, because a diagram of a self-contradictory document is still a useful
diagram, the same position `xlsx` and `dot` take.

## Testing

Golden files alone are not enough here, and it is worth being explicit about
why: a golden `.svg` tells you the bytes changed. It does not tell you the
drawing is wrong, and after a layout change every byte changes. The tests that
make a layout maintainable are the ones that assert properties.

* **Golden `.svg`** for the three existing fixtures — `full.json`, `edge.json`,
  `nofk.json` — with the `-update` flag, matching every other exporter. Add the
  package to the list in `DEVELOPERS.md`, since only the packages that own
  goldens define that flag.
* **Well-formedness**: parse the output with `encoding/xml`. The precedent is
  already in the repository — `internal/export/dot/golden_test.go` does exactly
  this to its HTML-like labels — and it is what catches choice 7 above.
* **Determinism**: render twice, compare.
* **Geometry invariants**, as the real regression net: no two table boxes
  overlap; every edge endpoint lies on the boundary of its box; every routed
  segment is axis-aligned; the viewBox contains every drawn coordinate; the
  number of drawn relationships equals the number of foreign keys.
* **A crossing-count ceiling** per fixture: assert the layout produces no more
  than a recorded number of edge crossings. This is what stops a later
  simplification from quietly turning the diagram into spaghetti while every
  other test stays green.
* **No renderer in CI.** Shelling out to `rsvg-convert` or a browser to check
  the output would reintroduce, in the test path, the dependency this whole
  exercise removes.

`edge.json` already carries most of the awkward cases — a self-reference, two
parallel relationships between the same pair, a stub target — so the fixture
work is largely done. Two gaps turned up while writing this, and both need
closing before the phases that depend on them can be trusted:

* **No cycle between two distinct tables.** Every reference in both fixtures is
  either acyclic or the `categories` self-loop. Phase 1 exists entirely for the
  mutual-reference case, and nothing in the tree exercises it.
* **`teeodot` appears in no golden file.** Counting the arrow types across both
  fixtures gives `crowtee`, `crowodot` and `teetee` but never `teeodot` — the
  one-and-optional end, a nullable foreign key whose columns are constrained
  unique. `internal/export/dot/cardinality.go` can produce it and no fixture
  asks it to. That is a gap in the **existing** DOT tests, not a new one, and
  it is worth fixing there first: it is much easier to see a wrong crow's foot
  in one line of DOT than in a drawn diagram.

## What it costs

Calibrating against what this repository has already chosen to write itself —
`internal/sml` is 1,604 lines of source for an XLSX writer, `internal/schema`
798 for a JSON Schema evaluator, `internal/importer/postgres` 3,363 for a SQL
parser:

| Part | Source, est. |
|---|---|
| `internal/erd` extraction | ~250 (moved, not new) |
| Text metrics, incl. the East Asian Width table | ~200 |
| Box sizing and theme | ~150 |
| Layered layout: phases 1–5 | ~450 |
| Routing, ports, lanes, self-loops | ~250 |
| SVG writer, markers, escaping | ~350 |
| **Total** | **~1,650 new** |

At this repository's roughly 1:1.3 source-to-test ratio, call it ~3,800 lines
all in. That makes it the second largest subsystem in jjf, behind the PostgreSQL
importer. It is not a weekend, and the estimate should be presented as such
before anyone starts.

## A staged plan

Each step leaves the tree green and is independently worth having.

1. **Extract `internal/erd`.** No behaviour change, dot goldens unchanged. Pays
   for itself immediately by killing the `renderType` duplication.
2. **Metrics, box sizing, SVG writer, and a trivial one-column layout.**
   Establishes the golden harness, the escaping, the C0 sanitiser, the
   well-formedness test and the theme. The output is ugly and correct.
3. **Layered layout, phases 1–5.** The diagram becomes a diagram.
4. **Routing, markers, labels, self-loops, parallel edges.** The diagram becomes
   an ER diagram.
5. **Docs.** `docs/usage.md` and `.ja`, both READMEs, the format table's usage
   text, `DEVELOPERS.md`'s golden-package list.

Steps 3 and 4 are where the estimate lives, and step 2 is where the harness
that makes them safe gets built. Resist the temptation to reorder.

## Does `dot` stay?

**Yes.** It is 558 lines that exist, pass and cost nothing to keep. A reader who
has graphviz gets better splines and PNG and PDF from it, and removing a format
is a breaking change traded for nothing. The claim `svg` makes is not that DOT
was a mistake; it is that jjf can hand you an image with nothing installed.

Keeping both also buys a free cross-check: the same document rendered twice
must agree about every crow's foot, and after the `internal/erd` extraction it
agrees by construction rather than by review.

## Open questions

* **Should `svg` become the format the documentation reaches for first?** It is
  the one that needs nothing. That is a documentation decision, taken once the
  output is good enough to stand behind, not now.
* **Per-column ports** — strictly more informative than the current output,
  and the point at which crossing reduction acquires port constraints. Revisit
  after step 4.
* **Where the top-out is.** Layered layout degrades on wide, dense schemas.
  Nobody has said how many tables a real jjf document has. Worth finding out
  before phase 2's ranking choice is defended, because "longest path is fine"
  is a claim about size.

## References

* Gansner, Koutsofios, North, Vo, *A Technique for Drawing Directed Graphs* —
  the framework `dot` and dagre both implement.
* Jünger, Mutzel, *2-Layer Straightline Crossing Minimization* — the survey to
  settle median against barycenter.
* Brandes, Köpf, *Fast and Simple Horizontal Coordinate Assignment*, and the
  2020 erratum, arXiv:2008.01252 — the two flaws, one previously undocumented.
* Eiglsperger, Siebenhaller, Kaufmann, *An Efficient Implementation of
  Sugiyama's Algorithm for Layered Graph Drawing*, GD 2004.
* dagre — https://github.com/dagrejs/dagre — the layered layout Mermaid uses to
  draw ER diagrams to SVG with no graphviz.
* github/markup#1160 — GitHub's SVG sanitiser removing `dominant-baseline`.
* librsvg #484 — `orient="auto-start-reverse"` unimplemented.
