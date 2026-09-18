package detect

// nodeDetector matches any generic Node project: a package.json exists
// and no more specific detector (Next.js) already claimed it. Checked
// after nextjsDetector in the default registry for exactly that reason.
type nodeDetector struct{}

func (nodeDetector) Detect(dir string) (*Result, error) {
	pkg, ok, err := readPackageJSON(dir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	if pkg.Scripts != nil {
		if _, hasStart := pkg.Scripts["start"]; hasStart {
			return &Result{Kind: KindNode, RunCommand: "npm start"}, nil
		}
	}
	// No start script -- there's no sensible default to guess at, so
	// report the kind but leave RunCommand empty and flag why.
	return &Result{Kind: KindNode, RunCommand: "", Detail: "no start script found in package.json"}, nil
}
