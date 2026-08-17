package ble

// toProperties maps BlueZ flag names to the noble property vocabulary the UI speaks.
func toProperties(flags []string) []string {
	m := map[string]string{
		"write-without-response": "writeWithoutResponse",
		"write":                  "write",
		"read":                   "read",
		"notify":                 "notify",
		"indicate":               "indicate",
		"broadcast":              "broadcast",
	}
	props := make([]string, 0, len(flags))
	for _, f := range flags {
		if mapped, ok := m[f]; ok {
			props = append(props, mapped)
		} else {
			props = append(props, f)
		}
	}
	return props
}
