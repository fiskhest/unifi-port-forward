package ports

import (
	"encoding/json"
	"strconv"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "single port", input: "8080", want: "8080"},
		{name: "range", input: "8000-8100", want: "8000-8100"},
		{name: "list of single ports", input: "80,443", want: "80,443"},
		{name: "mixed list", input: "80,443,8000-8100", want: "80,443,8000-8100"},
		{name: "whitespace tolerated", input: " 80 , 8000 - 8100 ", want: "80,8000-8100"},
		{name: "unsorted input is normalized", input: "443,80", want: "80,443"},
		{name: "overlapping segments merge", input: "8000-8100,8050-8200", want: "8000-8200"},
		{name: "adjacent segments merge", input: "80,81,82", want: "80-82"},
		{name: "duplicate segments collapse", input: "80,80", want: "80"},
		{name: "range covering one port collapses", input: "80-80", want: "80"},
		{name: "boundary ports", input: "1,65535", want: "1,65535"},

		{name: "empty", input: "", wantErr: true},
		{name: "blank", input: "   ", wantErr: true},
		{name: "empty segment", input: "80,,443", wantErr: true},
		{name: "trailing comma", input: "80,", wantErr: true},
		{name: "zero port", input: "0", wantErr: true},
		{name: "port above maximum", input: "65536", wantErr: true},
		{name: "range above maximum", input: "65000-65536", wantErr: true},
		{name: "inverted range", input: "8100-8000", wantErr: true},
		{name: "not a number", input: "http", wantErr: true},
		{name: "range with non-number", input: "80-http", wantErr: true},
		{name: "negative port reads as inverted range", input: "-80", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %q, want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) returned unexpected error: %v", tt.input, err)
			}
			if got.String() != tt.want {
				t.Errorf("Parse(%q).String() = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseRejectsMoreThanFifteenSegments(t *testing.T) {
	// Non-adjacent ports so nothing merges: 100, 200, 300, ...
	segments := func(n int) string {
		out := ""
		for i := 1; i <= n; i++ {
			if out != "" {
				out += ","
			}
			out += strconv.Itoa(i * 100)
		}
		return out
	}

	fifteen := segments(MaxSegments)
	if _, err := Parse(fifteen); err != nil {
		t.Errorf("Parse(%q) with %d segments returned error: %v", fifteen, MaxSegments, err)
	}

	sixteen := segments(MaxSegments + 1)
	if _, err := Parse(sixteen); err == nil {
		t.Errorf("Parse(%q) with %d segments should have failed", sixteen, MaxSegments+1)
	}
}

func TestSpecPredicates(t *testing.T) {
	single := MustParse("8080")
	contiguous := MustParse("8000-8100")
	list := MustParse("80,443")

	if !single.IsSinglePort() {
		t.Error("8080 should be a single port")
	}
	if contiguous.IsSinglePort() {
		t.Error("8000-8100 should not be a single port")
	}
	if !contiguous.IsContiguous() {
		t.Error("8000-8100 should be contiguous")
	}
	if list.IsContiguous() {
		t.Error("80,443 should not be contiguous")
	}
	if got := contiguous.Count(); got != 101 {
		t.Errorf("8000-8100 Count() = %d, want 101", got)
	}
	if got := list.Count(); got != 2 {
		t.Errorf("80,443 Count() = %d, want 2", got)
	}
	if got := list.Low(); got != 80 {
		t.Errorf("80,443 Low() = %d, want 80", got)
	}
	if (Spec{}).Low() != 0 {
		t.Error("empty spec Low() should be 0")
	}
	if !(Spec{}).IsEmpty() {
		t.Error("zero value should be empty")
	}
}

func TestSpecContains(t *testing.T) {
	spec := MustParse("80,8000-8100")

	for _, port := range []int{80, 8000, 8050, 8100} {
		if !spec.Contains(port) {
			t.Errorf("%s should contain %d", spec, port)
		}
	}
	for _, port := range []int{79, 81, 7999, 8101} {
		if spec.Contains(port) {
			t.Errorf("%s should not contain %d", spec, port)
		}
	}
}

func TestSpecEqual(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{a: "8080", b: "8080", want: true},
		{a: "80,81", b: "80-81", want: true},
		{a: "443,80", b: "80,443", want: true},
		{a: "8080", b: "8081", want: false},
		{a: "8000-8100", b: "8000-8101", want: false},
		{a: "80,443", b: "80", want: false},
	}

	for _, tt := range tests {
		if got := MustParse(tt.a).Equal(MustParse(tt.b)); got != tt.want {
			t.Errorf("%q.Equal(%q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSpecOverlaps(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{a: "8080", b: "8080", want: true},
		{a: "8000-8100", b: "8050", want: true},
		{a: "8000-8100", b: "8100-8200", want: true},
		{a: "80,443", b: "443,8080", want: true},
		{a: "8000-8100", b: "8101-8200", want: false},
		{a: "80,443", b: "81,444", want: false},
	}

	for _, tt := range tests {
		got := MustParse(tt.a).Overlaps(MustParse(tt.b))
		if got != tt.want {
			t.Errorf("%q.Overlaps(%q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
		// Overlap is symmetric
		if reverse := MustParse(tt.b).Overlaps(MustParse(tt.a)); reverse != tt.want {
			t.Errorf("%q.Overlaps(%q) = %v, want %v", tt.b, tt.a, reverse, tt.want)
		}
	}
}

func TestSpecAll(t *testing.T) {
	got := MustParse("80,8000-8002").All()
	want := []int{80, 8000, 8001, 8002}

	if len(got) != len(want) {
		t.Fatalf("All() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("All() = %v, want %v", got, want)
		}
	}
}

func TestContiguousFrom(t *testing.T) {
	spec, err := ContiguousFrom(8000, 101)
	if err != nil {
		t.Fatalf("ContiguousFrom(8000, 101) returned error: %v", err)
	}
	if spec.String() != "8000-8100" {
		t.Errorf("ContiguousFrom(8000, 101) = %q, want 8000-8100", spec)
	}

	if _, err := ContiguousFrom(65535, 2); err == nil {
		t.Error("ContiguousFrom(65535, 2) should fail: it runs past the maximum port")
	}
	if _, err := ContiguousFrom(80, 0); err == nil {
		t.Error("ContiguousFrom(80, 0) should fail: count must be positive")
	}
}

func TestUnion(t *testing.T) {
	got, err := Union(MustParse("80"), MustParse("443"), Spec{})
	if err != nil {
		t.Fatalf("Union returned error: %v", err)
	}
	if got.String() != "80,443" {
		t.Errorf("Union = %q, want 80,443", got)
	}

	empty, err := Union()
	if err != nil {
		t.Fatalf("Union() returned error: %v", err)
	}
	if !empty.IsEmpty() {
		t.Errorf("Union() = %q, want empty", empty)
	}
}

func TestParseOrEmpty(t *testing.T) {
	if got := ParseOrEmpty("8000-8100"); got.String() != "8000-8100" {
		t.Errorf("ParseOrEmpty(8000-8100) = %q", got)
	}
	for _, input := range []string{"", "nonsense", "0"} {
		if got := ParseOrEmpty(input); !got.IsEmpty() {
			t.Errorf("ParseOrEmpty(%q) = %q, want empty", input, got)
		}
	}
}

func TestSpecJSONRoundTrip(t *testing.T) {
	spec := MustParse("80,8000-8100")

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if string(data) != `"80,8000-8100"` {
		t.Errorf("Marshal = %s, want \"80,8000-8100\"", data)
	}

	var back Spec
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if !back.Equal(spec) {
		t.Errorf("round trip = %q, want %q", back, spec)
	}

	var empty Spec
	if err := json.Unmarshal([]byte(`""`), &empty); err != nil {
		t.Fatalf("Unmarshal of empty string returned error: %v", err)
	}
	if !empty.IsEmpty() {
		t.Errorf("Unmarshal of empty string = %q, want empty", empty)
	}
}

func TestRangesIsACopy(t *testing.T) {
	spec := MustParse("8000-8100")
	ranges := spec.Ranges()
	ranges[0].To = 9999

	if spec.String() != "8000-8100" {
		t.Errorf("mutating the result of Ranges() changed the spec: %q", spec)
	}
}

func TestFromPort(t *testing.T) {
	if got := FromPort(8080); got.String() != "8080" {
		t.Errorf("FromPort(8080) = %q, want 8080", got)
	}
}
