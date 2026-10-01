package session

import "strings"

// DeviceLabel names the browser and system a User-Agent header describes,
// for the session list (TKT-01M29CCC0X): "Firefox on Linux", "Safari on
// iPhone". It reads the header against two small tables and nothing else,
// with no lookup and no dependency, and answers "Unknown device" when
// neither table matches. A header is whatever the client sent, so the label
// is a hint for a member recognising their own devices and never a fact
// anything decides on.
func DeviceLabel(ua string) string {
	browser, system := firstMatch(ua, browsers), firstMatch(ua, systems)
	switch {
	case browser != "" && system != "":
		return browser + " on " + system
	case browser != "":
		return browser
	case system != "":
		return system
	}
	return "Unknown device"
}

// uaToken is a substring of a User-Agent and the name it stands for.
type uaToken struct{ needle, name string }

// browsers is checked in order, because browsers claim one another: Edge,
// Opera, Samsung Internet and Chromium say Chrome, Chrome says Safari, and
// the iOS browsers say Safari too.
var browsers = []uaToken{
	{"Edg/", "Edge"},
	{"EdgA/", "Edge"},
	{"EdgiOS/", "Edge"},
	{"OPR/", "Opera"},
	{"SamsungBrowser/", "Samsung Internet"},
	{"Firefox/", "Firefox"},
	{"FxiOS/", "Firefox"},
	{"CriOS/", "Chrome"},
	{"Chromium/", "Chromium"},
	{"Chrome/", "Chrome"},
	{"Version/", "Safari"},
}

// systems is checked in order: an iPhone says "like Mac OS X" and Android
// says Linux.
var systems = []uaToken{
	{"iPhone", "iPhone"},
	{"iPad", "iPad"},
	{"Android", "Android"},
	{"CrOS", "ChromeOS"},
	{"Windows", "Windows"},
	{"Macintosh", "macOS"},
	{"Mac OS X", "macOS"},
	{"Linux", "Linux"},
}

func firstMatch(ua string, table []uaToken) string {
	for _, t := range table {
		if strings.Contains(ua, t.needle) {
			return t.name
		}
	}
	return ""
}
