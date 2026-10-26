package composeedit

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// SetHAProxyPublishPorts replaces the haproxy service ports list.
// Other services and the rest of the haproxy service (volumes, logging,
// networks, limits) stay as they are. Parsing is YAML, not indentation.
func SetHAProxyPublishPorts(composeYAML string, ports []int) (string, error) {
	if len(ports) == 0 {
		return "", fmt.Errorf("ports list is empty")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(composeYAML), &doc); err != nil {
		return "", fmt.Errorf("parse compose: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", fmt.Errorf("compose root is not a mapping")
	}
	services := mapValue(doc.Content[0], "services")
	if services == nil {
		return "", fmt.Errorf("services not found")
	}
	hap := mapValue(services, "haproxy")
	if hap == nil {
		return "", fmt.Errorf("haproxy service not found")
	}
	portsNode := mapValue(hap, "ports")
	if portsNode == nil {
		return "", fmt.Errorf("haproxy ports: section not found in compose")
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, p := range ports {
		seq.Content = append(seq.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: fmt.Sprintf("%d:%d", p, p),
			Style: yaml.DoubleQuotedStyle,
		})
	}
	*portsNode = *seq

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func mapValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
