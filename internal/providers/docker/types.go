package docker

// composeProjectLabel is the label Docker Compose sets on every container
// it manages, naming the project ("docker compose -p <name>" or the
// directory name by default). We group by this label to show Compose
// projects in the UI -- no compose.yaml parsing needed, Compose already
// tells us this at the API level.
const composeProjectLabel = "com.docker.compose.project"

// Container is our normalized view of a container, built from whichever
// Docker Engine API shape we called (list vs. inspect use different JSON
// shapes upstream).
type Container struct {
	ID      string            `json:"id"`
	Names   []string          `json:"names"`
	Image   string            `json:"image"`
	Command string            `json:"command"`
	State   string            `json:"state"`
	Status  string            `json:"status"`
	Labels  map[string]string `json:"labels"`
	Ports   []Port            `json:"ports"`
	// Project is the com.docker.compose.project label value, or "" for a
	// container not managed by Compose.
	Project string `json:"project,omitempty"`
}

type Port struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort uint16 `json:"privatePort"`
	PublicPort  uint16 `json:"publicPort,omitempty"`
	Type        string `json:"type"`
}

// ContainerDetail is the richer view returned by InspectContainer.
type ContainerDetail struct {
	Container
	Env          []string `json:"env"`
	Mounts       []Mount  `json:"mounts"`
	RestartCount int      `json:"restartCount"`
}

type Mount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	RW          bool   `json:"rw"`
}

type Image struct {
	ID      string   `json:"id"`
	Tags    []string `json:"tags"`
	Size    int64    `json:"size"`
	Created int64    `json:"created"`
}

type Volume struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Labels     map[string]string `json:"labels"`
}

type Network struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Driver string            `json:"driver"`
	Scope  string            `json:"scope"`
	Labels map[string]string `json:"labels"`
}

// ComposeProject groups containers sharing a com.docker.compose.project
// label. Name == "" holds containers not managed by Compose.
type ComposeProject struct {
	Name       string      `json:"name"`
	Containers []Container `json:"containers"`
}

// GroupByComposeProject groups containers by their Compose project label,
// preserving first-seen order of projects. This is purely a client-side
// convenience over data the Engine API already returns; it never reads any
// compose.yaml file.
func GroupByComposeProject(containers []Container) []ComposeProject {
	order := make([]string, 0)
	groups := make(map[string][]Container)
	for _, ct := range containers {
		if _, ok := groups[ct.Project]; !ok {
			order = append(order, ct.Project)
		}
		groups[ct.Project] = append(groups[ct.Project], ct)
	}
	out := make([]ComposeProject, 0, len(order))
	for _, name := range order {
		out = append(out, ComposeProject{Name: name, Containers: groups[name]})
	}
	return out
}
