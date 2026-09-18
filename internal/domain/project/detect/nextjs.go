package detect

// nextjsDetector matches a Next.js application: package.json lists "next"
// as a dependency (or dev dependency -- some templates put it there).
type nextjsDetector struct{}

func (nextjsDetector) Detect(dir string) (*Result, error) {
	pkg, ok, err := readPackageJSON(dir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	_, inDeps := pkg.Dependencies["next"]
	_, inDevDeps := pkg.DevDependencies["next"]
	if !inDeps && !inDevDeps {
		return nil, nil
	}

	// Prefer the project's own "dev" script when it declares one (the
	// common case, and it respects any custom flags the project set up);
	// fall back to the bare `next dev` CLI invocation otherwise.
	runCommand := "next dev"
	if pkg.Scripts != nil {
		if _, hasDev := pkg.Scripts["dev"]; hasDev {
			runCommand = "npm run dev"
		}
	}
	return &Result{Kind: KindNextJS, RunCommand: runCommand}, nil
}
