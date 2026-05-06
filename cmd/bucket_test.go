package cmd

import (
	"strings"
	"testing"
)

// These tests pin the user-facing surface of `upuai bucket public` so that
// changes to flag wiring, subcommand registration, or copy are deliberate. We
// only assert structural properties (subcommands present, flags present, key
// phrases in help text) — not exact byte-for-byte snapshots, since cobra's
// formatting is sensitive to terminal width and we'd thrash on it.

func TestBucketCmd_HasPublicSubcommand(t *testing.T) {
	pub, _, err := bucketCmd.Find([]string{"public"})
	if err != nil {
		t.Fatalf("bucket has no `public` subcommand: %v", err)
	}
	if pub == nil || pub.Use != "public" {
		t.Fatalf("public command not registered correctly: %+v", pub)
	}

	// `enable` and `disable` must both be reachable from `bucket public`.
	for _, leaf := range []string{"enable", "disable"} {
		if c, _, err := bucketCmd.Find([]string{"public", leaf}); err != nil || c == nil {
			t.Errorf("missing leaf command `bucket public %s` (err=%v)", leaf, err)
		}
	}
}

func TestBucketCmd_HasBucketFlag(t *testing.T) {
	// Defining --bucket/-b on the parent makes every subcommand inherit it,
	// which the tests for resolveBucket() rely on. Lose this flag and the CLI
	// stops working for projects with >1 bucket.
	if f := bucketCmd.PersistentFlags().Lookup("bucket"); f == nil {
		t.Fatal("bucketCmd missing --bucket persistent flag")
	} else if f.Shorthand != "b" {
		t.Errorf("--bucket shorthand = %q, want b", f.Shorthand)
	}
}

func TestBucketCmd_HelpMentionsCanonicalUsage(t *testing.T) {
	// We assert on the user-facing copy that matters: that `enable`/`disable`
	// are documented in the long description, that --bucket is exposed in the
	// flag listing, and that the example block survives. Avoids byte-for-byte
	// snapshots which thrash on terminal width / cobra version bumps.
	long := bucketCmd.Long
	for _, want := range []string{
		"upuai bucket public",
		"enable",
		"disable",
		"--bucket",
	} {
		if !strings.Contains(long, want) {
			t.Errorf("bucket Long help missing %q\n--- long ---\n%s", want, long)
		}
	}

	// UsageString must enumerate the registered subcommands; if `public`
	// disappears from the docs we want to hear about it.
	usage := bucketCmd.UsageString()
	if !strings.Contains(usage, "public") {
		t.Errorf("bucket usage missing `public` subcommand listing\n--- usage ---\n%s", usage)
	}
}

func TestBucketPublicEnableCmd_HasYesFlagInheritance(t *testing.T) {
	// `enable` must honor a global --yes (bound at the root) so non-interactive
	// CI can skip the confirm. We don't redefine --yes on this command; we
	// rely on inheritance through cobra's persistent flag chain. If someone
	// breaks the wiring, the CLI silently always prompts and CI hangs.
	enable, _, err := bucketCmd.Find([]string{"public", "enable"})
	if err != nil {
		t.Fatalf("public enable not found: %v", err)
	}
	// Check inherited persistent flags include --yes from rootCmd.
	if f := enable.InheritedFlags().Lookup("yes"); f == nil {
		t.Error("enable command does not inherit --yes from root; non-interactive CI will hang on the Confirm prompt")
	}
}
