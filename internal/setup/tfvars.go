package setup

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"unicode"
)

// HomeVars are the inputs of the home stack that home.tfvars holds. The file can hold the
// required tags and the model key, so no error and no log line has a value of it.
type HomeVars struct {
	Env         string
	Region      string
	Network     string
	Subnets     []string
	RouterGroup string
	ZoneID      string
	Tags        map[string]string
	ModelKey    string
	// BedrockModels is the inference profile of Amazon Bedrock for each role. It takes the
	// place of ModelKey.
	BedrockModels map[string]string
	InstanceType  string
	// Application is the name of the pack, whose images have a repository in the stack.
	Application string
}

// tfvarsValue is one value of a tfvars file: a string, a list of strings or a map of
// strings. The home stack has no other kind of input.
type tfvarsValue struct {
	text  *string
	list  []string
	table map[string]string
}

// homeVarNames maps the variables of home.tfvars to their kind: s a string, l a list, m a
// map. The installer gives the realm and the two values of phase 2 itself.
var homeVarNames = map[string]byte{
	"env_name": 's', "region": 's', "vpc_id": 's', "private_subnet_ids": 'l',
	"router_security_group_id": 's', "zone_id": 's', "required_tags": 'm',
	"model_api_key": 's', "core_instance_type": 's', "application_name": 's',
	"bedrock_models": 'm',
}

// ReadHomeVars reads home.tfvars. A file that does not exist gives no values.
func ReadHomeVars(path string) (HomeVars, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return HomeVars{}, false, nil
	}
	if err != nil {
		return HomeVars{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	values, err := parseTfvars(string(data))
	if err != nil {
		return HomeVars{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	vars := HomeVars{}
	for name, value := range values {
		kind, known := homeVarNames[name]
		if !known {
			return HomeVars{}, false, fmt.Errorf("read %s: the home stack has no input %s: "+
				"remove it from the file", path, name)
		}
		if err := vars.set(name, kind, value); err != nil {
			return HomeVars{}, false, fmt.Errorf("read %s: %w", path, err)
		}
	}
	return vars, true, nil
}

func (v *HomeVars) set(name string, kind byte, value tfvarsValue) error {
	if value.text == nil && kind == 's' || value.list == nil && kind == 'l' ||
		value.table == nil && kind == 'm' {
		return fmt.Errorf("the input %s has a value of another kind", name)
	}
	switch name {
	case "env_name":
		v.Env = *value.text
	case "region":
		v.Region = *value.text
	case "vpc_id":
		v.Network = *value.text
	case "private_subnet_ids":
		v.Subnets = value.list
	case "router_security_group_id":
		v.RouterGroup = *value.text
	case "zone_id":
		v.ZoneID = *value.text
	case "required_tags":
		v.Tags = value.table
	case "model_api_key":
		v.ModelKey = *value.text
	case "bedrock_models":
		v.BedrockModels = value.table
	case "core_instance_type":
		v.InstanceType = *value.text
	case "application_name":
		v.Application = *value.text
	}
	return nil
}

// WriteHomeVars writes home.tfvars, readable only by its owner.
func WriteHomeVars(path string, v HomeVars) error {
	var b strings.Builder
	b.WriteString("# Written by `clanker install --cloud aws`. It holds the inputs of the home " +
		"stack.\n# Keep it private: it can hold the required tags and the model key.\n\n")
	line := func(name, value string) { fmt.Fprintf(&b, "%s = %s\n", name, value) }
	line("env_name", hclString(v.Env))
	line("region", hclString(v.Region))
	line("vpc_id", hclString(v.Network))
	quoted := make([]string, 0, len(v.Subnets))
	for _, subnet := range v.Subnets {
		quoted = append(quoted, hclString(subnet))
	}
	line("private_subnet_ids", "["+strings.Join(quoted, ", ")+"]")
	line("router_security_group_id", hclString(v.RouterGroup))
	line("zone_id", hclString(v.ZoneID))
	table(&b, "required_tags", v.Tags)
	if v.InstanceType != "" {
		line("core_instance_type", hclString(v.InstanceType))
	}
	if v.Application != "" {
		line("application_name", hclString(v.Application))
	}
	if v.ModelKey != "" {
		line("model_api_key", hclString(v.ModelKey))
	}
	if len(v.BedrockModels) > 0 {
		table(&b, "bedrock_models", v.BedrockModels)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	// WriteFile keeps the mode of a file that exists.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("make %s readable only by you: %w", path, err)
	}
	return nil
}

// table writes a map of strings, sorted by key.
func table(b *strings.Builder, name string, values map[string]string) {
	b.WriteString(name + " = {\n")
	for _, key := range slices.Sorted(maps.Keys(values)) {
		fmt.Fprintf(b, "  %s = %s\n", hclString(key), hclString(values[key]))
	}
	b.WriteString("}\n")
}

// hclString quotes a string for HCL. HCL reads ${ and %{ as the start of a template.
func hclString(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`,
		"${", "$${", "%{", "%%{")
	return `"` + replacer.Replace(value) + `"`
}

// tfvarsParser reads the part of the HCL syntax that a tfvars file of the home stack uses:
// attributes whose value is a quoted string, a list of strings or an object of strings,
// with comments. Its errors name the line and never a value.
type tfvarsParser struct {
	text string
	at   int
	line int
}

func parseTfvars(text string) (map[string]tfvarsValue, error) {
	p := &tfvarsParser{text: text, line: 1}
	values := map[string]tfvarsValue{}
	for {
		p.skip()
		if p.at >= len(p.text) {
			return values, nil
		}
		name, err := p.identifier()
		if err != nil {
			return nil, err
		}
		if _, seen := values[name]; seen {
			return nil, p.fail("the input %s is set twice", name)
		}
		p.skip()
		if !p.take('=') {
			return nil, p.fail("the input %s has no =", name)
		}
		value, err := p.value(name)
		if err != nil {
			return nil, err
		}
		values[name] = value
	}
}

func (p *tfvarsParser) fail(format string, args ...any) error {
	return fmt.Errorf("line %d: %s", p.line, fmt.Sprintf(format, args...))
}

// skip passes over spaces, line ends, commas and comments.
func (p *tfvarsParser) skip() {
	for p.at < len(p.text) {
		switch c := p.text[p.at]; {
		case c == '\n':
			p.line++
			p.at++
		case c == ' ' || c == '\t' || c == '\r' || c == ',':
			p.at++
		case c == '#' || strings.HasPrefix(p.text[p.at:], "//"):
			for p.at < len(p.text) && p.text[p.at] != '\n' {
				p.at++
			}
		case strings.HasPrefix(p.text[p.at:], "/*"):
			end := strings.Index(p.text[p.at+2:], "*/")
			if end < 0 {
				p.at = len(p.text)
				return
			}
			p.line += strings.Count(p.text[p.at:p.at+2+end], "\n")
			p.at += end + 4
		default:
			return
		}
	}
}

func (p *tfvarsParser) take(c byte) bool {
	if p.at < len(p.text) && p.text[p.at] == c {
		p.at++
		return true
	}
	return false
}

func (p *tfvarsParser) identifier() (string, error) {
	start := p.at
	for p.at < len(p.text) {
		r := rune(p.text[p.at])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			break
		}
		p.at++
	}
	if p.at == start {
		return "", p.fail("a name of an input is expected")
	}
	return p.text[start:p.at], nil
}

func (p *tfvarsParser) value(name string) (tfvarsValue, error) {
	p.skip()
	switch {
	case p.take('"'):
		text, err := p.rest(name)
		return tfvarsValue{text: &text}, err
	case p.take('['):
		list, err := p.list(name)
		return tfvarsValue{list: list}, err
	case p.take('{'):
		table, err := p.object(name)
		return tfvarsValue{table: table}, err
	}
	return tfvarsValue{}, p.fail("the value of %s is not a string, a list or a map", name)
}

// rest reads a quoted string after its first quote.
func (p *tfvarsParser) rest(name string) (string, error) {
	var b strings.Builder
	for p.at < len(p.text) {
		c := p.text[p.at]
		switch {
		case c == '"':
			p.at++
			return b.String(), nil
		case c == '\n':
			return "", p.fail("a string of %s does not end on its line", name)
		case c == '\\' && p.at+1 < len(p.text):
			escaped, ok := map[byte]string{'\\': `\`, '"': `"`, 'n': "\n", 'r': "\r",
				't': "\t"}[p.text[p.at+1]]
			if !ok {
				return "", p.fail("a string of %s has an escape that the installer does "+
					"not read", name)
			}
			b.WriteString(escaped)
			p.at += 2
		case strings.HasPrefix(p.text[p.at:], "$${") || strings.HasPrefix(p.text[p.at:], "%%{"):
			b.WriteString(p.text[p.at+1 : p.at+3])
			p.at += 3
		case strings.HasPrefix(p.text[p.at:], "${") || strings.HasPrefix(p.text[p.at:], "%{"):
			return "", p.fail("a string of %s has a template, which the installer does not "+
				"read", name)
		default:
			b.WriteByte(c)
			p.at++
		}
	}
	return "", p.fail("a string of %s does not end", name)
}

func (p *tfvarsParser) list(name string) ([]string, error) {
	list := []string{}
	for {
		p.skip()
		if p.take(']') {
			return list, nil
		}
		if !p.take('"') {
			return nil, p.fail("each item of %s must be a quoted string", name)
		}
		item, err := p.rest(name)
		if err != nil {
			return nil, err
		}
		list = append(list, item)
	}
}

func (p *tfvarsParser) object(name string) (map[string]string, error) {
	table := map[string]string{}
	for {
		p.skip()
		if p.take('}') {
			return table, nil
		}
		var key string
		var err error
		if p.take('"') {
			key, err = p.rest(name)
		} else {
			key, err = p.identifier()
		}
		if err != nil {
			return nil, err
		}
		p.skip()
		if !p.take('=') && !p.take(':') {
			return nil, p.fail("a key of %s has no = or :", name)
		}
		p.skip()
		if !p.take('"') {
			return nil, p.fail("each value of %s must be a quoted string", name)
		}
		if table[key], err = p.rest(name); err != nil {
			return nil, err
		}
	}
}
