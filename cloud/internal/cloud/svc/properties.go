package svc

import (
	"bufio"
	"strings"
)

// PropertyEntry represents one line in a properties file.
type PropertyEntry struct {
	Key              string
	Value            string
	RawLine          string
	Comment          string
	Delimiter        string // "=" or ":"
	IsCommentOrEmpty bool
}

// Properties represents an ordered collection of property entries,
// preserving comments, whitespace, and delimiter formatting.
type Properties struct {
	entries []PropertyEntry
}

// ParseProperties parses Java/Minecraft server.properties and .env style key=value files.
func ParseProperties(input string) *Properties {
	p := &Properties{
		entries: make([]PropertyEntry, 0),
	}

	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Empty line or comment line
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			p.entries = append(p.entries, PropertyEntry{
				RawLine:          line,
				Comment:          line,
				IsCommentOrEmpty: true,
			})
			continue
		}

		// Find delimiter: either '=' or ':'
		eqIdx := strings.Index(line, "=")
		colIdx := strings.Index(line, ":")
		delimIdx := -1
		delim := "="

		if eqIdx != -1 && colIdx != -1 {
			if eqIdx < colIdx {
				delimIdx = eqIdx
				delim = "="
			} else {
				delimIdx = colIdx
				delim = ":"
			}
		} else if eqIdx != -1 {
			delimIdx = eqIdx
			delim = "="
		} else if colIdx != -1 {
			delimIdx = colIdx
			delim = ":"
		}

		if delimIdx == -1 {
			// No delimiter found; treat as comment/raw
			p.entries = append(p.entries, PropertyEntry{
				RawLine:          line,
				Comment:          line,
				IsCommentOrEmpty: true,
			})
			continue
		}

		key := strings.TrimSpace(line[:delimIdx])
		val := strings.TrimSpace(line[delimIdx+1:])

		p.entries = append(p.entries, PropertyEntry{
			Key:              key,
			Value:            val,
			RawLine:          line,
			Delimiter:        delim,
			IsCommentOrEmpty: false,
		})
	}

	return p
}

// Get returns the value of a property and whether it exists.
func (p *Properties) Get(key string) (string, bool) {
	for _, e := range p.entries {
		if !e.IsCommentOrEmpty && e.Key == key {
			return e.Value, true
		}
	}
	return "", false
}

// Set updates an existing property value preserving original delimiter and line placement,
// or appends a new property to the end if not found.
func (p *Properties) Set(key, value string) {
	for i, e := range p.entries {
		if !e.IsCommentOrEmpty && e.Key == key {
			delim := e.Delimiter
			if delim == "" {
				delim = "="
			}
			p.entries[i].Value = value
			p.entries[i].RawLine = key + delim + value
			return
		}
	}

	// Not found, append
	p.entries = append(p.entries, PropertyEntry{
		Key:              key,
		Value:            value,
		Delimiter:        "=",
		RawLine:          key + "=" + value,
		IsCommentOrEmpty: false,
	})
}

// Delete removes a property entry by key.
func (p *Properties) Delete(key string) bool {
	found := false
	newEntries := make([]PropertyEntry, 0, len(p.entries))
	for _, e := range p.entries {
		if !e.IsCommentOrEmpty && e.Key == key {
			found = true
			continue
		}
		newEntries = append(newEntries, e)
	}
	if found {
		p.entries = newEntries
	}
	return found
}

// ToMap converts non-comment properties to a key-value map.
func (p *Properties) ToMap() map[string]string {
	m := make(map[string]string)
	for _, e := range p.entries {
		if !e.IsCommentOrEmpty {
			m[e.Key] = e.Value
		}
	}
	return m
}

// Serialize reconstructs the properties file preserving comments and formatting.
func (p *Properties) Serialize() string {
	var sb strings.Builder
	for i, e := range p.entries {
		if e.IsCommentOrEmpty {
			sb.WriteString(e.RawLine)
		} else {
			delim := e.Delimiter
			if delim == "" {
				delim = "="
			}
			sb.WriteString(e.Key)
			sb.WriteString(delim)
			sb.WriteString(e.Value)
		}
		if i < len(p.entries)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
