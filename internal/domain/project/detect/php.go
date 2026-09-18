package detect

// phpDetector matches any generic PHP project: a composer.json exists and
// no more specific detector (Laravel) already claimed it. Checked after
// laravelDetector in the default registry for exactly that reason. There
// is no single sensible default "run" command for an arbitrary PHP
// project (unlike Laravel's `php artisan serve`), so this is flagged as
// unknown/generic rather than guessing.
type phpDetector struct{}

func (phpDetector) Detect(dir string) (*Result, error) {
	_, ok, err := readComposerJSON(dir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &Result{Kind: KindPHP, RunCommand: "", Detail: "unknown/generic php"}, nil
}
