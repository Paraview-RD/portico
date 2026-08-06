package service

import "strings"

// Page is the limit/offset pair a list query runs with. Handlers translate
// the API's page/pageSize into this.
type Page struct {
	Limit  int
	Offset int
}

// escapeLike neutralizes the wildcards in a user-supplied search term so a
// keyword of "%" does not match every row.
//
// The queries that use this declare ESCAPE '\'.
func escapeLike(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	)
	return replacer.Replace(s)
}
