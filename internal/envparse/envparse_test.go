package envparse

import (
	"reflect"
	"testing"
)

func TestParse_BugReported_InlineComment(t *testing.T) {
	got := Parse("FOO=bar # this is foo\n")
	want := []ParsedVar{{Key: "FOO", Value: "bar"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_ExportPrefixStripped(t *testing.T) {
	got := Parse("export BAZ=qux\n")
	want := []ParsedVar{{Key: "BAZ", Value: "qux"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_QuotedHashIsLiteral(t *testing.T) {
	got := Parse(`QUOTED="hash # inside"` + "\n")
	want := []ParsedVar{{Key: "QUOTED", Value: "hash # inside"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_HashWithoutPrecedingSpaceIsLiteral(t *testing.T) {
	got := Parse("URL=https://example.com#hash\n")
	want := []ParsedVar{{Key: "URL", Value: "https://example.com#hash"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_SingleQuoteStrip(t *testing.T) {
	got := Parse(`FOO='val'` + "\n")
	want := []ParsedVar{{Key: "FOO", Value: "val"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_DoubleQuoteStrip(t *testing.T) {
	got := Parse(`FOO="val"` + "\n")
	want := []ParsedVar{{Key: "FOO", Value: "val"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_DoubleQuoteEscapes(t *testing.T) {
	got := Parse(`FOO="a\"b\nc"` + "\n")
	want := []ParsedVar{{Key: "FOO", Value: "a\"b\nc"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_EmptyValue(t *testing.T) {
	got := Parse("EMPTY=\n")
	want := []ParsedVar{{Key: "EMPTY", Value: ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_CommentLineSkipped(t *testing.T) {
	got := Parse("# this is a comment\nFOO=bar\n")
	want := []ParsedVar{{Key: "FOO", Value: "bar"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_BlankLinesSkipped(t *testing.T) {
	got := Parse("\n\n\nFOO=bar\n\n\n")
	want := []ParsedVar{{Key: "FOO", Value: "bar"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_InvalidKeySkipped(t *testing.T) {
	got := Parse("123FOO=bar\nVALID=ok\n")
	want := []ParsedVar{{Key: "VALID", Value: "ok"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_MissingEqualsSkipped(t *testing.T) {
	got := Parse("NO_EQUALS\nVALID=ok\n")
	want := []ParsedVar{{Key: "VALID", Value: "ok"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_DuplicateLastWins(t *testing.T) {
	got := Parse("FOO=a\nFOO=b\n")
	want := []ParsedVar{{Key: "FOO", Value: "b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_OrderPreserved(t *testing.T) {
	got := Parse("A=1\nB=2\nC=3\n")
	want := []ParsedVar{
		{Key: "A", Value: "1"},
		{Key: "B", Value: "2"},
		{Key: "C", Value: "3"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_CRLF(t *testing.T) {
	got := Parse("A=1\r\nB=2\r\n")
	want := []ParsedVar{
		{Key: "A", Value: "1"},
		{Key: "B", Value: "2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParse_WhitespaceAroundEquals(t *testing.T) {
	got := Parse("FOO  =  bar  \n")
	want := []ParsedVar{{Key: "FOO", Value: "bar"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestParseSingle_ValidAssignment(t *testing.T) {
	got, ok := ParseSingle("FOO=bar")
	if !ok || got.Key != "FOO" || got.Value != "bar" {
		t.Fatalf("expected (FOO=bar, true), got (%v, %v)", got, ok)
	}
}

func TestParseSingle_StripsInlineComment(t *testing.T) {
	got, ok := ParseSingle("FOO=bar # comment")
	if !ok || got.Key != "FOO" || got.Value != "bar" {
		t.Fatalf("expected FOO=bar, got %v", got)
	}
}

func TestParseSingle_StripsExportPrefix(t *testing.T) {
	got, ok := ParseSingle("export FOO=bar")
	if !ok || got.Key != "FOO" || got.Value != "bar" {
		t.Fatalf("expected FOO=bar, got %v", got)
	}
}

func TestParseSingle_RejectsMalformed(t *testing.T) {
	if _, ok := ParseSingle("not-an-assignment"); ok {
		t.Fatal("expected ok=false")
	}
	if _, ok := ParseSingle(""); ok {
		t.Fatal("expected ok=false for empty input")
	}
	if _, ok := ParseSingle("=novalue"); ok {
		t.Fatal("expected ok=false for missing key")
	}
}
