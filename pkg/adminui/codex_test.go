package adminui

import "testing"

func TestCodexSandboxTypeNormalizesLegacyAliases(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                   "danger-full-access",
		"dangerFullAccess":   "danger-full-access",
		"workspaceWrite":     "workspace-write",
		"readOnly":           "read-only",
		"danger-full-access": "danger-full-access",
	}

	for input, want := range cases {
		if got := codexSandboxType(input); got != want {
			t.Fatalf("codexSandboxType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildTextInputItemUsesDirectTextVariant(t *testing.T) {
	t.Parallel()

	item := buildTextInputItem("diagnose tunnel state")
	if got := item["type"]; got != "text" {
		t.Fatalf("type = %#v, want %q", got, "text")
	}
	if got := item["text"]; got != "diagnose tunnel state" {
		t.Fatalf("text = %#v, want %q", got, "diagnose tunnel state")
	}
	if _, ok := item["content"]; ok {
		t.Fatalf("content should be omitted for text variant")
	}
}
