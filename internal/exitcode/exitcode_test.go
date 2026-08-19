package exitcode

import (
	"errors"
	"fmt"
	"testing"
)

func TestOf(t *testing.T) {
	base := errors.New("boom")

	tests := []struct {
		name string
		err  error
		want Code
	}{
		{"nil is OK", nil, OK},
		{"unclassified is General", base, General},
		{"wrapped invalid input", Wrap(InvalidInput, "read", base), InvalidInput},
		{"wrapped schema failure", Wrap(SchemaFailed, "validate", base), SchemaFailed},
		{"wrapped output failure", Wrap(OutputFailed, "export", base), OutputFailed},
		{"through fmt.Errorf chain", fmt.Errorf("export xlsx: %w", Wrap(OutputFailed, "write", base)), OutputFailed},
		{"through two chain levels", fmt.Errorf("a: %w", fmt.Errorf("b: %w", Wrap(SchemaFailed, "", base))), SchemaFailed},
		{"outermost code wins", Wrap(InvalidInput, "outer", Wrap(SchemaFailed, "inner", base)), InvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Of(tt.err); got != tt.want {
				t.Errorf("Of(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestWrapNilStaysNil(t *testing.T) {
	if err := Wrap(General, "", nil); err != nil {
		t.Fatalf("Wrap(General, \"\", nil) = %v, want nil", err)
	}
	if err := Wrap(OutputFailed, "export", nil); err != nil {
		t.Fatalf("Wrap(OutputFailed, \"export\", nil) = %v, want nil", err)
	}
}

func TestErrorMessage(t *testing.T) {
	base := errors.New("boom")

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"with op", Wrap(InvalidInput, "read config", base), "read config: boom"},
		{"without op", Wrap(InvalidInput, "", base), "boom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnwrapKeepsChain(t *testing.T) {
	base := errors.New("boom")
	err := fmt.Errorf("outer: %w", Wrap(SchemaFailed, "validate", base))
	if !errors.Is(err, base) {
		t.Errorf("errors.Is(err, base) = false, want true")
	}

	var ce *CodedError
	if !errors.As(err, &ce) {
		t.Fatalf("errors.As(err, &ce) = false, want true")
	}
	if ce.Op != "validate" {
		t.Errorf("ce.Op = %q, want %q", ce.Op, "validate")
	}
}
