// Package ports models UniFi's dst_port / fwd_port fields, which accept a single
// port ("8080"), an inclusive range ("8000-8100"), or a comma-separated list of
// either ("80,443,8000-8100").
//
// A Spec is always normalized: segments are sorted, and overlapping or adjacent
// segments are merged. Two specs describing the same set of ports therefore have
// the same String(), which lets the controller compare desired state against
// router state without caring how the ports were originally written.
package ports

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	// MinPort and MaxPort bound a valid TCP/UDP port.
	MinPort = 1
	MaxPort = 65535

	// MaxSegments is UniFi's cap on comma-separated elements in dst_port/fwd_port.
	MaxSegments = 15
)

// Range is an inclusive port range. A single port is a Range with From == To.
type Range struct {
	From int
	To   int
}

// Count is the number of ports the range covers.
func (r Range) Count() int { return r.To - r.From + 1 }

func (r Range) String() string {
	if r.From == r.To {
		return strconv.Itoa(r.From)
	}
	return strconv.Itoa(r.From) + "-" + strconv.Itoa(r.To)
}

// Spec is a normalized set of ports.
type Spec struct {
	ranges []Range
}

// FromPort builds a spec covering a single port.
func FromPort(port int) Spec {
	return Spec{ranges: []Range{{From: port, To: port}}}
}

// FromRange builds a spec covering an inclusive range.
func FromRange(from, to int) (Spec, error) {
	return New(Range{From: from, To: to})
}

// New normalizes and validates a set of ranges.
func New(ranges ...Range) (Spec, error) {
	if len(ranges) == 0 {
		return Spec{}, fmt.Errorf("port spec is empty")
	}

	normalized := make([]Range, 0, len(ranges))
	for _, r := range ranges {
		if r.From < MinPort || r.From > MaxPort {
			return Spec{}, fmt.Errorf("port %d out of range (%d-%d)", r.From, MinPort, MaxPort)
		}
		if r.To < MinPort || r.To > MaxPort {
			return Spec{}, fmt.Errorf("port %d out of range (%d-%d)", r.To, MinPort, MaxPort)
		}
		if r.From > r.To {
			return Spec{}, fmt.Errorf("range %d-%d is inverted: start must not exceed end", r.From, r.To)
		}
		normalized = append(normalized, r)
	}

	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].From != normalized[j].From {
			return normalized[i].From < normalized[j].From
		}
		return normalized[i].To < normalized[j].To
	})

	// Merge overlapping and adjacent segments so equal port sets compare equal.
	merged := []Range{normalized[0]}
	for _, r := range normalized[1:] {
		last := &merged[len(merged)-1]
		if r.From <= last.To+1 {
			if r.To > last.To {
				last.To = r.To
			}
			continue
		}
		merged = append(merged, r)
	}

	if len(merged) > MaxSegments {
		return Spec{}, fmt.Errorf("port spec has %d segments, UniFi allows at most %d", len(merged), MaxSegments)
	}

	return Spec{ranges: merged}, nil
}

// Parse reads a UniFi port spec: "8080", "8000-8100", or "80,443,8000-8100".
func Parse(s string) (Spec, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return Spec{}, fmt.Errorf("port spec is empty")
	}

	var ranges []Range
	for segment := range strings.SplitSeq(trimmed, ",") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return Spec{}, fmt.Errorf("port spec %q has an empty segment", s)
		}

		from, to, found := strings.Cut(segment, "-")
		if !found {
			port, err := parsePort(segment)
			if err != nil {
				return Spec{}, fmt.Errorf("port spec %q: %w", s, err)
			}
			ranges = append(ranges, Range{From: port, To: port})
			continue
		}

		low, err := parsePort(strings.TrimSpace(from))
		if err != nil {
			return Spec{}, fmt.Errorf("port spec %q: %w", s, err)
		}
		high, err := parsePort(strings.TrimSpace(to))
		if err != nil {
			return Spec{}, fmt.Errorf("port spec %q: %w", s, err)
		}
		ranges = append(ranges, Range{From: low, To: high})
	}

	spec, err := New(ranges...)
	if err != nil {
		return Spec{}, fmt.Errorf("port spec %q: %w", s, err)
	}
	return spec, nil
}

// MustParse reads a port spec and panics if it is invalid. It is meant for
// literals known to be valid at author time, such as in tests.
func MustParse(s string) Spec {
	spec, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return spec
}

// ParseOrEmpty reads a port spec, yielding the empty spec instead of an error.
// It suits places that read back router state, where an unparseable field means
// "no ports we recognize" rather than a failure worth propagating.
func ParseOrEmpty(s string) Spec {
	spec, err := Parse(s)
	if err != nil {
		return Spec{}
	}
	return spec
}

// Union merges specs into one, dropping any that are empty.
func Union(specs ...Spec) (Spec, error) {
	var ranges []Range
	for _, spec := range specs {
		ranges = append(ranges, spec.ranges...)
	}
	if len(ranges) == 0 {
		return Spec{}, nil
	}
	return New(ranges...)
}

// ContiguousFrom builds a spec of n consecutive ports starting at base.
func ContiguousFrom(base, n int) (Spec, error) {
	if n < 1 {
		return Spec{}, fmt.Errorf("port count must be positive, got %d", n)
	}
	end := base + n - 1
	if end > MaxPort {
		return Spec{}, fmt.Errorf("%d ports starting at %d would run past port %d", n, base, MaxPort)
	}
	return FromRange(base, end)
}

func parsePort(s string) (int, error) {
	port, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a port number", s)
	}
	if port < MinPort || port > MaxPort {
		return 0, fmt.Errorf("port %d out of range (%d-%d)", port, MinPort, MaxPort)
	}
	return port, nil
}

// IsEmpty reports whether the spec covers no ports.
func (s Spec) IsEmpty() bool { return len(s.ranges) == 0 }

// Ranges returns the normalized segments.
func (s Spec) Ranges() []Range {
	out := make([]Range, len(s.ranges))
	copy(out, s.ranges)
	return out
}

// Count is the total number of ports covered.
func (s Spec) Count() int {
	total := 0
	for _, r := range s.ranges {
		total += r.Count()
	}
	return total
}

// IsSinglePort reports whether the spec covers exactly one port.
func (s Spec) IsSinglePort() bool {
	return len(s.ranges) == 1 && s.ranges[0].From == s.ranges[0].To
}

// IsContiguous reports whether the spec is one unbroken run of ports.
func (s Spec) IsContiguous() bool { return len(s.ranges) == 1 }

// Low is the lowest port in the spec, or 0 when the spec is empty. It is the
// representative port used for logs, events and rule names.
func (s Spec) Low() int {
	if s.IsEmpty() {
		return 0
	}
	return s.ranges[0].From
}

// Contains reports whether the spec covers the given port.
func (s Spec) Contains(port int) bool {
	for _, r := range s.ranges {
		if port >= r.From && port <= r.To {
			return true
		}
	}
	return false
}

// Equal reports whether both specs cover exactly the same ports.
func (s Spec) Equal(other Spec) bool {
	if len(s.ranges) != len(other.ranges) {
		return false
	}
	for i, r := range s.ranges {
		if r != other.ranges[i] {
			return false
		}
	}
	return true
}

// Overlaps reports whether the two specs share at least one port.
func (s Spec) Overlaps(other Spec) bool {
	for _, a := range s.ranges {
		for _, b := range other.ranges {
			if a.From <= b.To && b.From <= a.To {
				return true
			}
		}
	}
	return false
}

// All returns every port the spec covers, in ascending order.
func (s Spec) All() []int {
	out := make([]int, 0, s.Count())
	for _, r := range s.ranges {
		for port := r.From; port <= r.To; port++ {
			out = append(out, port)
		}
	}
	return out
}

// String renders the spec in UniFi's format.
func (s Spec) String() string {
	if s.IsEmpty() {
		return ""
	}
	segments := make([]string, 0, len(s.ranges))
	for _, r := range s.ranges {
		segments = append(segments, r.String())
	}
	return strings.Join(segments, ",")
}

// MarshalJSON renders the spec as its string form, so structured logs and event
// payloads show "8000-8100" rather than an opaque object.
func (s Spec) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON reads the string form produced by MarshalJSON.
func (s *Spec) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if strings.TrimSpace(raw) == "" {
		*s = Spec{}
		return nil
	}
	parsed, err := Parse(raw)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}
