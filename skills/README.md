# jjf Agent Skill

[日本語](README.ja.md)

One Agent Skill for editing and validating jjf database design JSON, and for
regenerating the Excel design document from it.

| Skill | Directory | Invoke as |
| --- | --- | --- |
| `db-design` | [`db-design/`](db-design/SKILL.md) | `/jjf:db-design` |

The skill is written in English. It carries Japanese trigger keywords as well, so
a request written in Japanese loads it too; the agent then answers in the language
you wrote in.

To read or review the same content in Japanese — the conventions, the type
vocabulary, the error table and the editing recipes — see
[`docs/db-design-guide.ja.md`](../docs/db-design-guide.ja.md), a guide written in
Japanese for readers who want it in that language. It is written for people, not
for agents: the agent reads the English skill, which wins whenever the two
disagree.

## Install as a plugin

This is the recommended way. The skill is packaged as the Claude Code plugin
`jjf`, published by the marketplace `jjf-tools`.

```text
/plugin marketplace add https://raw.githubusercontent.com/shutx-net/jumping-json-flush/main/.claude-plugin/marketplace.json
/plugin install jjf@jjf-tools
```

Then invoke it with `/jjf:db-design`, or just describe the change you want and
let Claude load it by itself.

**No git, no npm, no Node on your machine.** The plugin is delivered as a zip
archive fetched over HTTPS, so nothing has to be cloned or installed from a
package registry. The archive is pinned by its SHA-256 digest, which Claude Code
verifies on every download.

**Requires Claude Code v2.1.224 or later.** That is the version that added the
`archive` plugin source. On v2.1.120 through v2.1.223 the install is refused with
`This plugin uses a source type your Claude Code version does not support`; on
older versions the marketplace does not load at all.

The marketplace URL points at [the default
branch](../.claude-plugin/marketplace.json), not at a release asset. That is
deliberate: a catalog URL carrying a tag would pin whoever added it to that one
release forever, and no `/plugin marketplace update` could move them off it. The
entry inside it is what names the release, and it is installable from the first
tagged release onward.

To move to a newer release:

```text
/plugin marketplace update jjf-tools
/plugin update jjf@jjf-tools
```

## Install by copying the directory

The alternative, for a repository that would rather commit the skill than depend
on a marketplace. Copy the whole directory: `SKILL.md` and `references/`
together.

As a project skill, available to everyone working in that repository and
committed with it:

```sh
mkdir -p /path/to/your-repo/.claude/skills
cp -r skills/db-design /path/to/your-repo/.claude/skills/
```

As a personal skill, available in every project on that machine and not committed
anywhere:

```sh
mkdir -p ~/.claude/skills
cp -r skills/db-design ~/.claude/skills/
```

Copied this way the skill is invoked as `/db-design`, without the `jjf:`
prefix, and it does not receive updates.

## Check the installation

`/plugin` lists the installed plugins and lets you enable or disable them.
`/skills` lists the skills Claude Code has discovered, however they were
installed. `claude doctor` validates the frontmatter of each one.

## Requirements

The skill drives the `jjf` CLI, which must be on `PATH`. The plugin does not
ship the binary — it is a skill, not an installer. See the repository
[README](../README.md) for installation.

Its frontmatter uses only the fields that every Agent Skills host understands —
`name`, `description`, `license`, `compatibility`, `allowed-tools` and `metadata`
— so the same directory also works as a claude.ai skill upload and with the
Anthropic Agent SDK.

`allowed-tools` pre-approves `Read` and the two `jjf` subcommands only —
`validate`, which reads, and `export`, which writes a workbook at a path the user
supplies. Editing the JSON still goes through the usual permission prompt, on
purpose: pre-approving `Write` and `Edit` would be a privilege escalation for
whoever installs the skill.

## Releasing the plugin

For maintainers. The archive is built and published by
[`.github/workflows/release.yml`](../.github/workflows/release.yml) on a `v*`
tag, as `jjf-plugin-<tag>.zip`, alongside the CLI binaries. Its digest also
lands in `checksums.txt`.

1. In one commit, set `version` in
   [`.claude-plugin/plugin.json`](../.claude-plugin/plugin.json) and in the
   plugin entry of
   [`.claude-plugin/marketplace.json`](../.claude-plugin/marketplace.json) to the
   new version without its leading `v`, point `source.url` at the new tag's
   asset, and **delete `source.sha256`**. A digest left over from the previous
   release is worse than none: it makes every install fail the integrity check,
   while an absent one merely installs unpinned. The release job refuses to
   publish if any of this is inconsistent with the tag.
2. Tag and push. The job builds the archive, computes its digest, publishes the
   release, and prints the complete next content of `marketplace.json` in its job
   summary. The same file is attached to the run as the `marketplace-json`
   artifact.
3. Commit that content to the default branch. Installs are unpinned until you do.
   The digest is reproducible, so re-running the job for the same tag produces the
   same value.

The job never pushes to the default branch itself, and `marketplace.json` is
never published as a release asset.

## Reference

- Agent Skills documentation: <https://code.claude.com/docs/en/skills.md>
- Plugin reference: <https://code.claude.com/docs/en/plugins-reference.md>
- Plugin marketplaces: <https://code.claude.com/docs/en/plugin-marketplaces.md>
