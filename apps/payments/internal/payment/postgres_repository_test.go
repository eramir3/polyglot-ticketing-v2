package payment

import "testing"

func TestParseProcessorOutcome(t *testing.T) {
	for _, testCase := range []struct {
		input string
		want  Status
	}{
		{input: "", want: StatusSucceeded},
		{input: "success", want: StatusSucceeded},
		{input: "failure", want: StatusFailed},
	} {
		got, err := ParseProcessorOutcome(testCase.input)
		if err != nil || got != testCase.want {
			t.Fatalf("ParseProcessorOutcome(%q) = %q, %v; want %q, nil", testCase.input, got, err, testCase.want)
		}
	}
	if _, err := ParseProcessorOutcome("unknown"); err == nil {
		t.Fatal("expected invalid processor outcome to fail")
	}
}
