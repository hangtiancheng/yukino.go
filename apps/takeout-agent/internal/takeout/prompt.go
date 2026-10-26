package takeout

import "embed"

// promptFS embeds the English prompt templates at compile time so the
// binary is self-contained, mirroring the Java classpath resources.
//
//go:embed all:prompts
var promptFS embed.FS

// PromptLoader reads embedded prompt templates.
type PromptLoader struct{}

// Load returns the prompt template stored under the given embedded path,
// e.g. "prompts/intent.txt".
func (PromptLoader) Load(path string) (string, error) {
	data, err := promptFS.ReadFile(path)
	if err != nil {
		return "", WrapError(err, "load prompt failed: %s", path)
	}
	return string(data), nil
}

// MustLoad returns the prompt template or panics; used at wiring time for
// the fixed set of embedded prompts.
func (l PromptLoader) MustLoad(path string) string {
	text, err := l.Load(path)
	if err != nil {
		panic(err)
	}
	return text
}
