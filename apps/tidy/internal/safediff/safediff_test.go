package safediff

import (
	"strings"
	"testing"
)

func TestAcceptsPlainWikilinkInsertion(t *testing.T) {
	orig := "今天学了一阶ODE,属于微分方程的一种。"
	with := "今天学了[[一阶ODE]],属于[[微分方程]]的一种。"
	got, err := CheckBodyWithLinks(orig, with)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != with {
		t.Fatalf("got %q want %q", got, with)
	}
}

func TestAcceptsAliasedWikilink(t *testing.T) {
	orig := "I covered ordinary differential equations today."
	with := "I covered [[Ordinary Differential Equations|ordinary differential equations]] today."
	if _, err := CheckBodyWithLinks(orig, with); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestAcceptsSectionLink(t *testing.T) {
	orig := "See the linear ODE section."
	with := "See the [[ODE#linear|linear ODE]] section."
	if _, err := CheckBodyWithLinks(orig, with); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestRejectsRewording(t *testing.T) {
	orig := "I covered ordinary differential equations today."
	with := "Today I covered [[Ordinary Differential Equations]]."
	if _, err := CheckBodyWithLinks(orig, with); err == nil {
		t.Fatal("expected rejection (reordering)")
	}
}

func TestRejectsContentInsertion(t *testing.T) {
	orig := "Notes on ODE."
	with := "Notes on [[ODE]]. Also some extra commentary."
	if _, err := CheckBodyWithLinks(orig, with); err == nil {
		t.Fatal("expected rejection (extra prose)")
	}
}

func TestRejectsContentDeletion(t *testing.T) {
	orig := "Notes on ODE today, very long writeup."
	with := "Notes on [[ODE]]."
	if _, err := CheckBodyWithLinks(orig, with); err == nil {
		t.Fatal("expected rejection (deletion)")
	}
}

func TestRejectsLinkInsideFence(t *testing.T) {
	orig := "```py\nimport ode\nsolve(ode)\n```\n"
	with := "```py\nimport [[ode]]\nsolve(ode)\n```\n"
	_, err := CheckBodyWithLinks(orig, with)
	if err == nil {
		t.Fatal("expected rejection (wikilink inside code fence)")
	}
	if !strings.Contains(err.Error(), "code fence") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAllowsLinkOutsideFence(t *testing.T) {
	orig := "Solve ODEs:\n```py\nimport ode\n```\n"
	with := "Solve [[ODE|ODEs]]:\n```py\nimport ode\n```\n"
	if _, err := CheckBodyWithLinks(orig, with); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestTrimsTrailingWhitespace(t *testing.T) {
	orig := "Hello world.\n\n"
	with := "Hello [[World|world]].\n"
	if _, err := CheckBodyWithLinks(orig, with); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
