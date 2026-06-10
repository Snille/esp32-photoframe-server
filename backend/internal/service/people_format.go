package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
)

// Person is one face recognized in a photo, as stored in Image.PeopleJSON.
type Person struct {
	Name      string `json:"name"`
	BirthDate string `json:"birthDate"`
}

// truncateRunes shortens s to maxLen runes, appending an ellipsis (and trimming
// a trailing space/comma) when cut. maxLen is assumed already normalized.
func truncateRunes(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if maxLen <= 1 {
		return string(runes[:maxLen])
	}
	return strings.TrimRight(string(runes[:maxLen-1]), " ,") + "…"
}

// FormatLocation truncates a location string to its rune budget.
func FormatLocation(location string, maxLen int) string {
	return truncateRunes(location, model.NormalizeLocationMaxLen(maxLen))
}

// FormatDescription truncates a photo description to its rune budget.
func FormatDescription(description string, maxLen int) string {
	return truncateRunes(description, model.NormalizeDescriptionMaxLen(maxLen))
}

// ParsePeople decodes the Image.PeopleJSON blob. Returns nil on empty/invalid.
func ParsePeople(peopleJSON string) []Person {
	if strings.TrimSpace(peopleJSON) == "" {
		return nil
	}
	var people []Person
	if err := json.Unmarshal([]byte(peopleJSON), &people); err != nil {
		return nil
	}
	return people
}

// firstRune returns the first rune of s as an uppercase string (handles
// non-ASCII initials like "Å"), or "" for empty input.
func firstRune(s string) string {
	for _, r := range s {
		return strings.ToUpper(string(r))
	}
	return ""
}

// formatName renders a single person's name per the chosen format key.
func formatName(name, format string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	fields := strings.Fields(name)
	first := fields[0]
	last := ""
	if len(fields) > 1 {
		last = fields[len(fields)-1]
	}

	switch format {
	case "first":
		return first
	case "first_initial":
		if last == "" {
			return first
		}
		return first + " " + firstRune(last) + "."
	case "last":
		if last == "" {
			return first
		}
		return last
	case "last_first":
		if last == "" {
			return first
		}
		return last + " " + first
	case "last_initial":
		if last == "" {
			return first
		}
		return last + " " + firstRune(first) + "."
	default: // first_last
		if last == "" {
			return first
		}
		return first + " " + last
	}
}

// ageAt computes a person's whole-year age at ref from an Immich birthDate
// ("YYYY-MM-DD" or RFC3339). Returns (age, true) when computable and >= 0.
func ageAt(birthDate string, ref time.Time) (int, bool) {
	birthDate = strings.TrimSpace(birthDate)
	if birthDate == "" {
		return 0, false
	}
	var b time.Time
	var err error
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05.000Z"} {
		if b, err = time.Parse(layout, birthDate); err == nil {
			break
		}
	}
	if err != nil {
		return 0, false
	}
	age := ref.Year() - b.Year()
	// Subtract a year if the birthday hasn't occurred yet by ref's month/day.
	if ref.Month() < b.Month() || (ref.Month() == b.Month() && ref.Day() < b.Day()) {
		age--
	}
	if age < 0 {
		return 0, false
	}
	return age, true
}

// FormatPeople renders the people overlay string: each name in the chosen
// format, optionally with the age (at photoDate) in parentheses, joined by
// commas. Whole names are kept until maxLen is reached; any remainder collapses
// to a trailing "+N". Returns "" when there are no people.
func FormatPeople(peopleJSON string, photoDate *time.Time, format string, showAge bool, maxLen int) string {
	people := ParsePeople(peopleJSON)
	if len(people) == 0 {
		return ""
	}
	format = model.NormalizeNameFormat(format)
	maxLen = model.NormalizeNamesMaxLen(maxLen)

	ref := time.Now()
	if photoDate != nil {
		ref = *photoDate
	}

	parts := make([]string, 0, len(people))
	for _, p := range people {
		label := formatName(p.Name, format)
		if label == "" {
			continue
		}
		if showAge {
			if age, ok := ageAt(p.BirthDate, ref); ok {
				label += " (" + strconv.Itoa(age) + ")"
			}
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return ""
	}

	// Greedily keep whole names that fit within maxLen (counted in runes, so
	// accented names budget correctly); collapse the rest to a trailing +N.
	out := ""
	used := 0
	for i, part := range parts {
		candidate := part
		if i > 0 {
			candidate = ", " + part
		}
		clen := utf8.RuneCountInString(candidate)
		if used+clen > maxLen && out != "" {
			remaining := len(parts) - i
			return out + " +" + strconv.Itoa(remaining)
		}
		out += candidate
		used += clen
	}
	return out
}
