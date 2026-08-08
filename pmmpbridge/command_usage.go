package dragonfly

import (
	"strconv"
	"strings"
	"unicode"

	pmmpcompat "velaris-dragonfly/pmmpclient"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/go-gl/mathgl/mgl64"
)

type staticEnum struct {
	typeName string
	options  []string
}

func (e staticEnum) Type() string                { return e.typeName }
func (e staticEnum) Options(cmd.Source) []string { return e.options }

func pmmpCommandRunnables(runtime *Runtime, label string, info pmmpcompat.CommandInfo) []cmd.Runnable {
	overloads := structuredPMMPOverloads(info.Overloads)
	if len(overloads) == 0 {
		overloads = parsePMMPUsage(info.Name, info.Usage)
	}
	if len(overloads) == 0 {
		return []cmd.Runnable{pmmpCommand{runtime: runtime, label: label}}
	}
	runnables := make([]cmd.Runnable, 0, len(overloads))
	for _, overload := range overloads {
		runnables = append(runnables, pmmpCommand{runtime: runtime, label: label, params: overload})
	}
	return runnables
}

func structuredPMMPOverloads(overloads []pmmpcompat.CommandOverloadInfo) [][]cmd.ParamInfo {
	if len(overloads) == 0 {
		return nil
	}
	out := make([][]cmd.ParamInfo, 0, len(overloads))
	for _, overload := range overloads {
		if len(overload.Parameters) == 0 {
			continue
		}
		params := make([]cmd.ParamInfo, 0, len(overload.Parameters))
		used := map[string]int{}
		for _, parameter := range overload.Parameters {
			param := structuredPMMPParam(parameter)
			param.Name = uniqueParamName(param.Name, used)
			params = append(params, param)
		}
		out = append(out, params)
	}
	return out
}

func structuredPMMPParam(parameter pmmpcompat.CommandParameterInfo) cmd.ParamInfo {
	name := cleanParamName(parameter.Name)
	if name == "" {
		name = "value"
	}
	value := structuredPMMPParamValue(parameter)
	return cmd.ParamInfo{Name: name, Value: value, Optional: parameter.Optional}
}

func structuredPMMPParamValue(parameter pmmpcompat.CommandParameterInfo) any {
	if parameter.Subcommand {
		return cmd.SubCommand{}
	}
	if len(parameter.EnumValues) > 0 {
		if len(parameter.EnumValues) == 1 && strings.EqualFold(parameter.EnumValues[0], parameter.Name) {
			return cmd.SubCommand{}
		}
		typeName := parameter.EnumName
		if typeName == "" {
			typeName = "Enum:" + cleanParamName(parameter.Name)
		}
		return staticEnum{typeName: typeName, options: parameter.EnumValues}
	}
	typeName := strings.ToLower(strings.TrimSpace(parameter.TypeName))
	switch {
	case parameter.Type == 1 || strings.Contains(typeName, "int") || strings.Contains(typeName, "number"):
		return int(0)
	case parameter.Type == 3 || strings.Contains(typeName, "float") || strings.Contains(typeName, "decimal") || strings.Contains(typeName, "double"):
		return float64(0)
	case parameter.Type == 5 || strings.Contains(typeName, "target") || strings.Contains(typeName, "player"):
		return []cmd.Target{}
	case parameter.Type == 6 || strings.Contains(typeName, "x y z") || strings.Contains(typeName, "position"):
		return mgl64.Vec3{}
	case parameter.Type == 7 || strings.Contains(typeName, "text") || strings.Contains(typeName, "raw"):
		return cmd.Varargs("")
	default:
		return ""
	}
}

func parsePMMPUsage(name, usage string) [][]cmd.ParamInfo {
	var overloads [][]cmd.ParamInfo
	for _, line := range usageLines(usage) {
		tokens := strings.Fields(stripUsageCommand(name, line))
		if len(tokens) == 0 {
			continue
		}
		params := make([]cmd.ParamInfo, 0, len(tokens))
		used := map[string]int{}
		for i, token := range tokens {
			descriptionStarts := strings.HasSuffix(token, ":")
			token = strings.TrimSuffix(token, ":")
			param, ok := usageTokenParam(token, i == len(tokens)-1)
			if !ok {
				continue
			}
			param.Name = uniqueParamName(param.Name, used)
			params = append(params, param)
			if descriptionStarts {
				break
			}
		}
		overloads = append(overloads, params)
	}
	return overloads
}

func usageLines(usage string) []string {
	usage = strings.TrimSpace(usage)
	if usage == "" {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(usage, "\n") {
		for _, part := range strings.Split(line, "| /") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !strings.HasPrefix(part, "/") && strings.Contains(line, "| /") {
				part = "/" + part
			}
			lines = append(lines, part)
		}
	}
	return lines
}

func stripUsageCommand(name, line string) string {
	line = stripCommandFormatting(line)
	line = strings.TrimSpace(strings.TrimPrefix(line, "Usage:"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "usage:"))
	line = strings.TrimSpace(line)
	line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "•"))
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	first := strings.TrimPrefix(fields[0], "/")
	if strings.EqualFold(strings.TrimSuffix(first, ":"), name) {
		if strings.HasSuffix(first, ":") {
			return ""
		}
		return strings.Join(fields[1:], " ")
	}
	return line
}

func usageTokenParam(token string, last bool) (cmd.ParamInfo, bool) {
	token = strings.TrimSpace(strings.Trim(token, ","))
	if token == "" {
		return cmd.ParamInfo{}, false
	}
	optional := false
	required := false
	if strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">") {
		required = true
		token = strings.TrimSuffix(strings.TrimPrefix(token, "<"), ">")
	} else if strings.HasPrefix(token, "[") && strings.HasSuffix(token, "]") {
		optional = true
		token = strings.TrimSuffix(strings.TrimPrefix(token, "["), "]")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return cmd.ParamInfo{}, false
	}

	name, typeHint := splitUsageToken(token)
	if !required && !optional && !strings.Contains(token, "|") {
		return cmd.ParamInfo{Name: cleanParamName(name), Value: cmd.SubCommand{}}, true
	}
	value := usageParamValue(name, typeHint, strings.Contains(token, "...") || last)
	return cmd.ParamInfo{Name: cleanParamName(name), Value: value, Optional: optional}, true
}

func splitUsageToken(token string) (string, string) {
	if before, after, ok := strings.Cut(token, ":"); ok {
		return strings.TrimSpace(before), strings.TrimSpace(after)
	}
	if strings.Contains(token, "|") {
		options := strings.Split(token, "|")
		return strings.TrimSpace(options[0]), token
	}
	return strings.Trim(token, "."), ""
}

func usageParamValue(name, typeHint string, last bool) any {
	hint := strings.ToLower(strings.TrimSpace(typeHint + " " + name))
	if strings.Contains(typeHint, "|") {
		options := strings.Split(typeHint, "|")
		cleaned := make([]string, 0, len(options))
		for _, option := range options {
			option = strings.TrimSpace(strings.Trim(option, "<>[]"))
			if option != "" {
				cleaned = append(cleaned, option)
			}
		}
		if len(cleaned) > 0 {
			return staticEnum{typeName: "Enum:" + cleanParamName(name), options: cleaned}
		}
	}
	switch {
	case strings.Contains(hint, "...") || (last && (strings.Contains(hint, "message") || strings.Contains(hint, "reason") || strings.Contains(hint, "args") || strings.Contains(hint, "text"))):
		return cmd.Varargs("")
	case strings.Contains(hint, "bool"):
		return false
	case strings.Contains(hint, "float"), strings.Contains(hint, "double"), strings.Contains(hint, "decimal"):
		return float64(0)
	case strings.Contains(hint, "int"), strings.Contains(hint, "amount"), strings.Contains(hint, "count"), strings.Contains(hint, "level"), strings.Contains(hint, "page"), strings.Contains(hint, "number"), strings.Contains(hint, "radius"), strings.Contains(hint, "size"):
		return int(0)
	case strings.Contains(hint, "player"), strings.Contains(hint, "target"):
		return []cmd.Target{}
	default:
		return ""
	}
}

func cleanParamName(name string) string {
	name = strings.TrimSpace(strings.Trim(name, "<>[]().,"))
	if name == "" {
		return "value"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "value"
	}
	return b.String()
}

func stripCommandFormatting(s string) string {
	var b strings.Builder
	skipNext := false
	for _, r := range s {
		if skipNext {
			skipNext = false
			continue
		}
		switch r {
		case '§':
			skipNext = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func uniqueParamName(name string, used map[string]int) string {
	if name == "" {
		name = "value"
	}
	used[name]++
	if used[name] == 1 {
		return name
	}
	return name + "_" + strconv.Itoa(used[name])
}
