package aptekamos

import "strings"

var translitDigraphs = []struct{ lat, cyr string }{
	{"shch", "щ"}, {"sch", "щ"}, {"dzh", "дж"}, {"zh", "ж"}, {"kh", "х"}, {"ts", "ц"}, {"ch", "ч"}, {"sh", "ш"},
	{"yu", "ю"}, {"ya", "я"}, {"yo", "ё"}, {"ye", "е"}, {"ph", "ф"}, {"ck", "к"}, {"th", "т"}, {"ee", "и"}, {"oo", "у"},
}

var translitLetters = map[rune]string{
	'a': "а", 'b': "б", 'c': "к", 'd': "д", 'e': "е", 'f': "ф", 'g': "г", 'h': "х", 'i': "и", 'j': "ж", 'k': "к",
	'l': "л", 'm': "м", 'n': "н", 'o': "о", 'p': "п", 'q': "к", 'r': "р", 's': "с", 't': "т", 'u': "у", 'v': "в",
	'w': "в", 'x': "кс", 'y': "и", 'z': "з",
}

// IsLatinOnly reports whether s has Latin letters and no Cyrillic ones.
func IsLatinOnly(s string) bool {
	latin := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z':
			latin = true
		case r >= 'а' && r <= 'я', r == 'ё':
			return false
		}
	}
	return latin
}

// Transliterate spells a Latin name the way a Russian reader would write it ("sedzharo" -> "седжаро").
// Characters other than Latin letters are kept.
func Transliterate(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for len(s) > 0 {
		matched := false
		for _, d := range translitDigraphs {
			if strings.HasPrefix(s, d.lat) {
				b.WriteString(d.cyr)
				s = s[len(d.lat):]
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		r := []rune(s)[0]
		s = s[len(string(r)):]
		if c, ok := translitLetters[r]; ok {
			// c before e, i, y sounds like ц
			if r == 'c' && len(s) > 0 && strings.ContainsRune("eiy", rune(s[0])) {
				c = "ц"
			}
			b.WriteString(c)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
