// Package detect implements VoltPanel's framework-detection engine (§17
// Phase 3 of the implementation plan): given a project directory, identify
// which framework/runtime it looks like and suggest a sensible default run
// command.
//
// Detectors are registered in an ordered list rather than hardcoded into a
// switch statement, so adding a new framework later means writing one more
// Detector implementation and appending it to the registry -- no existing
// detector needs to change.
package detect

// Kind identifies the detected project type.
type Kind string

const (
	KindLaravel Kind = "laravel"
	KindNextJS  Kind = "nextjs"
	KindNode    Kind = "node"
	KindPHP     Kind = "php"
	KindUnknown Kind = "unknown"
)

// Result is what a Detector (or the top-level Detect) reports.
type Result struct {
	Kind Kind `json:"kind"`
	// RunCommand is the default command VoltPanel would use to start this
	// project's dev server. Empty when no definitive default exists (e.g.
	// a bare composer.json with no framework markers) -- callers should
	// treat an empty RunCommand as "ask the user," not silently run
	// nothing.
	RunCommand string `json:"runCommand"`
	// Detail explains ambiguous/unknown results.
	Detail string `json:"detail,omitempty"`
}

// Detector inspects a project directory and reports whether it matches.
// Detect returns (nil, nil) when dir doesn't match this detector's
// framework; it returns a non-nil error only for genuine I/O failures
// reading dir (a missing marker file is "no match," not an error).
type Detector interface {
	Detect(dir string) (*Result, error)
}

// defaultDetectors is the built-in, ordered registry. Order matters: more
// specific frameworks (Laravel, Next.js) are checked before the generic
// language-level fallbacks (plain PHP, plain Node) that would otherwise
// also match on the same marker files (composer.json / package.json).
var defaultDetectors = []Detector{
	laravelDetector{},
	nextjsDetector{},
	nodeDetector{},
	phpDetector{},
}

// Register appends a new detector to the default registry, checked after
// every detector already registered. This is the extension point Phase
// 3's plan entry asks for ("extensible list, not a hardcoded switch that's
// painful to add to later") -- adding a framework later means writing one
// Detector and calling Register, never editing this package's existing
// detectors.
func Register(d Detector) {
	defaultDetectors = append(defaultDetectors, d)
}

// Detect runs every registered detector in order against dir and returns
// the first match, or KindUnknown if none matched.
func Detect(dir string) (Result, error) {
	return DetectWith(defaultDetectors, dir)
}

// DetectWith runs a specific detector list against dir -- exported mainly
// so tests can exercise a subset/ordering without mutating the package's
// global registry.
func DetectWith(detectors []Detector, dir string) (Result, error) {
	for _, d := range detectors {
		res, err := d.Detect(dir)
		if err != nil {
			return Result{}, err
		}
		if res != nil {
			return *res, nil
		}
	}
	return Result{Kind: KindUnknown, Detail: "no known framework markers found"}, nil
}
