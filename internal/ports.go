package internal

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultPortHostIP  = "127.0.0.1"
	defaultPortProto   = "tcp"
	portRangeErrorText = "port ranges are not yet supported; list ports explicitly: --port 3000,3001,3002"
)

// PortChange represents the result of parsing user input against existing state.
type PortChange struct {
	Replace bool          // true if the input started with "="
	Add     []PortMapping // new mappings to add
	Remove  []PortMapping // existing mappings to remove (matched by ContainerPort + Protocol)
}

// ParsePortChange parses one or more --port values (each may itself be
// comma-separated) against the project's current ports. Returns the
// resolved final set and an error.
func ParsePortChange(specs []string, current []PortMapping) ([]PortMapping, error) {
	change, err := parsePortChange(specs, current)
	if err != nil {
		return nil, err
	}
	if change.Replace {
		return append([]PortMapping(nil), change.Add...), nil
	}

	final := append([]PortMapping(nil), current...)
	for _, add := range change.Add {
		if portMappingIndex(final, add) != -1 {
			continue
		}
		final = append(final, add)
	}
	for _, remove := range change.Remove {
		idx := portMappingIndex(final, remove)
		if idx == -1 {
			return nil, fmt.Errorf("port %s is not currently mapped", formatContainerPortProtocol(remove))
		}
		final = append(final[:idx], final[idx+1:]...)
	}
	return final, nil
}

// ParsePortList parses port specs without add/remove/replace semantics.
// Used by --port-range and by tests. Errors on - or = prefixes.
func ParsePortList(specs []string) ([]PortMapping, error) {
	tokens, err := splitPortSpecs(specs)
	if err != nil {
		return nil, err
	}
	ports := make([]PortMapping, 0, len(tokens))
	seen := map[string]struct{}{}
	for _, token := range tokens {
		switch {
		case strings.HasPrefix(token, "-"):
			return nil, fmt.Errorf("port list does not accept - prefix")
		case strings.HasPrefix(token, "="):
			return nil, fmt.Errorf("port list does not accept = prefix")
		case strings.HasPrefix(token, "+"):
			token = strings.TrimPrefix(token, "+")
		}
		if token == "" {
			return nil, fmt.Errorf("empty port spec")
		}
		port, err := parsePortMapping(token)
		if err != nil {
			return nil, err
		}
		key := containerPortProtocolKey(port)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ports = append(ports, port)
	}
	return ports, nil
}

// PortMappingsEqual returns true if two slices represent the same set,
// ignoring order. Matching is by (HostIP, HostPort, ContainerPort, Protocol).
func PortMappingsEqual(a, b []PortMapping) bool {
	aset := fullPortMappingSet(a)
	bset := fullPortMappingSet(b)
	if len(aset) != len(bset) {
		return false
	}
	for key := range aset {
		if _, ok := bset[key]; !ok {
			return false
		}
	}
	return true
}

// FormatPortMapping renders one mapping in compact human form.
func FormatPortMapping(p PortMapping) string {
	hostIP := p.HostIP
	if hostIP == "" {
		hostIP = defaultPortHostIP
	}
	proto := normalizedPortProtocol(p.Protocol)

	var out string
	switch {
	case hostIP != defaultPortHostIP:
		out = fmt.Sprintf("%s:%s:%s", hostIP, p.HostPort, p.ContainerPort)
	case p.HostPort != p.ContainerPort:
		out = fmt.Sprintf("%s:%s", p.HostPort, p.ContainerPort)
	default:
		out = p.ContainerPort
	}
	if proto != defaultPortProto {
		out += "/" + proto
	}
	return out
}

// FormatPortList joins mappings with ", " in sorted order.
func FormatPortList(ports []PortMapping) string {
	if len(ports) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		parts = append(parts, FormatPortMapping(port))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func parsePortChange(specs []string, current []PortMapping) (PortChange, error) {
	tokens, err := splitPortSpecs(specs)
	if err != nil {
		return PortChange{}, err
	}
	var change PortChange
	addByPort := map[string]PortMapping{}
	removeByPort := map[string]PortMapping{}
	var removeOrder []PortMapping

	for i, token := range tokens {
		mode := byte('+')
		spec := token
		if strings.HasPrefix(token, "=") {
			if i != 0 {
				return PortChange{}, fmt.Errorf("replace marker (=) may only appear on the first port token")
			}
			change.Replace = true
			spec = strings.TrimPrefix(token, "=")
		} else if strings.HasPrefix(token, "+") {
			spec = strings.TrimPrefix(token, "+")
		} else if strings.HasPrefix(token, "-") {
			if change.Replace {
				return PortChange{}, fmt.Errorf("cannot use - prefix in replace mode (=)")
			}
			mode = '-'
			spec = strings.TrimPrefix(token, "-")
		}

		if spec == "" {
			return PortChange{}, fmt.Errorf("empty port spec")
		}
		if change.Replace && mode == '-' {
			return PortChange{}, fmt.Errorf("cannot use - prefix in replace mode (=)")
		}

		port, err := parsePortMapping(spec)
		if err != nil {
			return PortChange{}, err
		}
		key := containerPortProtocolKey(port)
		if mode == '-' {
			if add, ok := addByPort[key]; ok {
				return PortChange{}, fmt.Errorf("port %s cannot be both added and removed in one command", formatContainerPortConflict(add))
			}
			if _, ok := removeByPort[key]; !ok {
				removeOrder = append(removeOrder, port)
			}
			removeByPort[key] = port
			continue
		}
		if remove, ok := removeByPort[key]; ok {
			return PortChange{}, fmt.Errorf("port %s cannot be both added and removed in one command", formatContainerPortConflict(remove))
		}
		if _, ok := addByPort[key]; ok {
			continue
		}
		addByPort[key] = port
		change.Add = append(change.Add, port)
	}

	if change.Replace {
		return change, nil
	}

	for _, remove := range removeOrder {
		idx := portMappingIndex(current, remove)
		if idx == -1 {
			return PortChange{}, fmt.Errorf("port %s is not currently mapped", formatContainerPortProtocol(remove))
		}
		change.Remove = append(change.Remove, current[idx])
	}
	return change, nil
}

func splitPortSpecs(specs []string) ([]string, error) {
	var tokens []string
	for _, spec := range specs {
		for _, token := range strings.Split(spec, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				return nil, fmt.Errorf("empty port spec")
			}
			tokens = append(tokens, token)
		}
	}
	return tokens, nil
}

func parsePortMapping(spec string) (PortMapping, error) {
	base, proto, err := splitPortProtocol(spec)
	if err != nil {
		return PortMapping{}, err
	}
	if containsPortRange(base) {
		return PortMapping{}, fmt.Errorf(portRangeErrorText)
	}

	parts := strings.Split(base, ":")
	if len(parts) < 1 || len(parts) > 3 {
		return PortMapping{}, fmt.Errorf("invalid port spec %q", spec)
	}

	hostIP := defaultPortHostIP
	hostPort := ""
	containerPort := ""
	switch len(parts) {
	case 1:
		containerPort = parts[0]
		hostPort = containerPort
	case 2:
		hostPort = parts[0]
		containerPort = parts[1]
	case 3:
		hostIP = parts[0]
		hostPort = parts[1]
		containerPort = parts[2]
	}

	if hostIP == "" {
		hostIP = defaultPortHostIP
	} else if net.ParseIP(hostIP) == nil {
		return PortMapping{}, fmt.Errorf("invalid host IP %q", hostIP)
	}
	if err := validatePortNumber("container port", containerPort, 1, 65535); err != nil {
		return PortMapping{}, err
	}
	if err := validatePortNumber("host port", hostPort, 0, 65535); err != nil {
		return PortMapping{}, err
	}

	return PortMapping{
		HostIP:        hostIP,
		HostPort:      hostPort,
		ContainerPort: containerPort,
		Protocol:      proto,
	}, nil
}

func splitPortProtocol(spec string) (string, string, error) {
	base, proto, ok := strings.Cut(spec, "/")
	if strings.Contains(proto, "/") {
		return "", "", fmt.Errorf("invalid port protocol in %q", spec)
	}
	if !ok {
		return spec, defaultPortProto, nil
	}
	proto = strings.ToLower(strings.TrimSpace(proto))
	if proto != "tcp" && proto != "udp" {
		return "", "", fmt.Errorf("invalid port protocol %q", proto)
	}
	return base, proto, nil
}

func validatePortNumber(label, value string, min, max int) error {
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid %s %q", label, value)
	}
	if n < min || n > max {
		return fmt.Errorf("%s %q must be between %d and %d", label, value, min, max)
	}
	return nil
}

func containsPortRange(spec string) bool {
	for _, part := range strings.Split(spec, ":") {
		lo, hi, ok := strings.Cut(part, "-")
		if ok && lo != "" && hi != "" && allDigits(lo) && allDigits(hi) {
			return true
		}
	}
	return false
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func portMappingIndex(ports []PortMapping, target PortMapping) int {
	targetKey := containerPortProtocolKey(target)
	for i, port := range ports {
		if containerPortProtocolKey(port) == targetKey {
			return i
		}
	}
	return -1
}

func containerPortProtocolKey(p PortMapping) string {
	return p.ContainerPort + "/" + normalizedPortProtocol(p.Protocol)
}

func fullPortMappingSet(ports []PortMapping) map[string]struct{} {
	set := map[string]struct{}{}
	for _, port := range ports {
		key := strings.Join([]string{
			port.HostIP,
			port.HostPort,
			port.ContainerPort,
			port.Protocol,
		}, "\x00")
		set[key] = struct{}{}
	}
	return set
}

func normalizedPortProtocol(proto string) string {
	if proto == "" {
		return defaultPortProto
	}
	return strings.ToLower(proto)
}

func formatContainerPortProtocol(p PortMapping) string {
	return p.ContainerPort + "/" + normalizedPortProtocol(p.Protocol)
}

func formatContainerPortConflict(p PortMapping) string {
	if normalizedPortProtocol(p.Protocol) == defaultPortProto {
		return p.ContainerPort
	}
	return formatContainerPortProtocol(p)
}
