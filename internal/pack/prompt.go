package pack

import "strings"

// Render replaces the placeholders of a system prompt with the names of the pack. The
// values are names that Validate checked against a fixed pattern, so a pack cannot put
// its own sentences into a prompt.
//
//	{{releases_repository}}  the repository with the release notes
//	{{releases_directory}}   the directory of the notes in that repository
//	{{components}}           the components, each with its deployment, as "a, b or c"
//	{{mitigations}}          the runbooks that are mitigations for an incident, or "none"
func (p Pack) Render(prompt string) string {
	return strings.NewReplacer(
		"{{releases_repository}}", p.Releases.Repository,
		"{{releases_directory}}", p.Releases.Directory,
		"{{components}}", oneOf(p.componentsWithWorkloads()),
		"{{mitigations}}", p.mitigationNames(),
	).Replace(prompt)
}

// componentsWithWorkloads returns each component as "api (the deployment shop-api)".
func (p Pack) componentsWithWorkloads() []string {
	names := make([]string, 0, len(p.Components))
	for _, component := range p.Components {
		names = append(names, component.Name+" (the deployment "+component.Workload+")")
	}
	return names
}

// mitigationNames returns the names of the mitigation runbooks as "a, b", or the word
// "none" when the pack has none.
func (p Pack) mitigationNames() string {
	if len(p.mitigations) == 0 {
		return "none"
	}
	return strings.Join(p.mitigations, ", ")
}

// oneOf writes a list as "a, b or c".
func oneOf(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	last := len(names) - 1
	return strings.Join(names[:last], ", ") + " or " + names[last]
}
