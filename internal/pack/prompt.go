package pack

import "strings"

// Render replaces the placeholders of a system prompt with the names of the pack. The
// values are names that Validate checked against a fixed pattern, so a pack cannot put
// its own sentences into a prompt.
//
//	{{releases_repository}}  the repository with the release notes
//	{{releases_directory}}   the directory of the notes in that repository
//	{{deployments}}          the deployments, as "a, b or c"
func (p Pack) Render(prompt string) string {
	return strings.NewReplacer(
		"{{releases_repository}}", p.Releases.Repository,
		"{{releases_directory}}", p.Releases.Directory,
		"{{deployments}}", oneOf(p.Deployments),
	).Replace(prompt)
}

// oneOf writes a list as "a, b or c".
func oneOf(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	last := len(names) - 1
	return strings.Join(names[:last], ", ") + " or " + names[last]
}
