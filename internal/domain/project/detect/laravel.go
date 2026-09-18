package detect

// laravelDetector matches a Laravel application: either the `artisan`
// console script sits at the project root, or composer.json declares a
// require on laravel/framework. Either marker alone is enough -- a fresh
// Laravel skeleton has both, but a minimal fixture only needs one.
type laravelDetector struct{}

func (laravelDetector) Detect(dir string) (*Result, error) {
	isLaravel := fileExists(dir, "artisan")

	composer, ok, err := readComposerJSON(dir)
	if err != nil {
		return nil, err
	}
	if !isLaravel && ok {
		if _, hasLaravel := composer.Require["laravel/framework"]; hasLaravel {
			isLaravel = true
		}
	}

	if !isLaravel {
		return nil, nil
	}
	return &Result{Kind: KindLaravel, RunCommand: "php artisan serve"}, nil
}
