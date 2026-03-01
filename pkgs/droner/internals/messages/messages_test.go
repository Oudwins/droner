package messages

import "testing"

func TestToRawText(t *testing.T) {
	t.Parallel()

	if got := ToRawText(nil); got != "" {
		t.Fatalf("ToRawText(nil) = %q, want empty", got)
	}

	m := &Message{
		Role: MessageRoleUser,
		Parts: []MessagePart{
			NewTextPart("hello"),
			{Type: PartType("image"), Text: "ignored"},
			NewTextPart(""),
			NewTextPart("world"),
		},
	}

	if got, want := ToRawText(m), "hello\nworld"; got != want {
		t.Fatalf("ToRawText(m) = %q, want %q", got, want)
	}
}
