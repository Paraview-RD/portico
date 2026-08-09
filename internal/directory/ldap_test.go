package directory

import (
	"errors"
	"testing"

	"github.com/go-ldap/ldap/v3"
)

// entry builds the shape go-ldap hands back, with raw bytes rather than
// strings, because the distinction is the entire subject of these tests.
func entry(dn string, attributes map[string][]byte) *ldap.Entry {
	e := &ldap.Entry{DN: dn}
	for name, value := range attributes {
		e.Attributes = append(e.Attributes, &ldap.EntryAttribute{
			Name:       name,
			Values:     []string{string(value)},
			ByteValues: [][]byte{value},
		})
	}
	return e
}

// Active Directory's objectGUID is sixteen raw bytes, and the reconciliation
// key is built from it.
//
// Read as text it becomes mojibake — which still *works*, in the sense that
// nothing errors and accounts import. It fails later and silently: the bytes
// decode differently depending on what is in them, so the same account can
// produce a different key on a later sync, and a rename in the directory
// then creates a second account here instead of renaming the first. That is
// precisely the failure this feature exists to prevent, so the expected
// value below is written out in full rather than computed.
//
// The GUID's first three groups are little-endian and the last two are not.
// That inconsistency is in Microsoft's format, not in this code, and getting
// it wrong produces an identifier that matches nothing an operator can look
// up when they are trying to work out why an account did not sync.
func TestActiveDirectoryGUIDBecomesTheIdentifierOperatorsSee(t *testing.T) {
	// {6B29FC40-CA47-1067-B31D-00DD010662DA} as AD stores it on the wire.
	raw := []byte{
		0x40, 0xfc, 0x29, 0x6b,
		0x47, 0xca,
		0x67, 0x10,
		0xb3, 0x1d,
		0x00, 0xdd, 0x01, 0x06, 0x62, 0xda,
	}

	client := &Client{cfg: Config{Attributes: AttributeMap{
		Username: "sAMAccountName", DisplayName: "displayName", ExternalID: "objectGUID",
	}}}

	mapped, err := client.mapEntry(entry("CN=Zhang San,DC=example,DC=org", map[string][]byte{
		"sAMAccountName": []byte("zhangsan"),
		"displayName":    []byte("张三"),
		"objectGUID":     raw,
	}))
	if err != nil {
		t.Fatalf("map entry: %v", err)
	}

	const want = "6b29fc40-ca47-1067-b31d-00dd010662da"
	if mapped.ExternalID != want {
		t.Errorf("externalId = %q, want %q\n"+
			"This value is the reconciliation key. If it is wrong or unstable, "+
			"a rename in the directory becomes a second account here, silently.",
			mapped.ExternalID, want)
	}
}

// OpenLDAP's entryUUID is already printable text and must pass through
// untouched — not be mistaken for something needing conversion.
func TestOpenLDAPUUIDPassesThroughUnchanged(t *testing.T) {
	client := &Client{cfg: Config{Attributes: AttributeMap{
		Username: "uid", DisplayName: "cn", ExternalID: "entryUUID",
	}}}

	const uuid = "1f2e3d4c-5b6a-7988-9a0b-1c2d3e4f5061"
	mapped, err := client.mapEntry(entry("uid=lisi,dc=example,dc=org", map[string][]byte{
		"uid":       []byte("lisi"),
		"cn":        []byte("Li Si"),
		"entryUUID": []byte(uuid),
	}))
	if err != nil {
		t.Fatalf("map entry: %v", err)
	}
	if mapped.ExternalID != uuid {
		t.Errorf("externalId = %q, want %q unchanged", mapped.ExternalID, uuid)
	}
}

// A sixteen-character printable identifier is text, not a GUID. Without this
// distinction a directory using short string ids would have them mangled
// into GUIDs on the way in.
func TestSixteenPrintableCharactersAreNotAGUID(t *testing.T) {
	client := &Client{cfg: Config{Attributes: AttributeMap{
		Username: "uid", DisplayName: "cn", ExternalID: "employeeNumber",
	}}}

	const id = "EMP-0000000-0042" // exactly 16 characters
	mapped, err := client.mapEntry(entry("uid=wangwu,dc=example,dc=org", map[string][]byte{
		"uid":            []byte("wangwu"),
		"cn":             []byte("Wang Wu"),
		"employeeNumber": []byte(id),
	}))
	if err != nil {
		t.Fatalf("map entry: %v", err)
	}
	if mapped.ExternalID != id {
		t.Errorf("externalId = %q, want %q; a printable identifier was "+
			"mistaken for a binary GUID", mapped.ExternalID, id)
	}
}

// An entry with no identifier is skipped, not imported under a made-up one.
func TestEntryWithoutAnIdentifierIsRefused(t *testing.T) {
	client := &Client{cfg: Config{Attributes: AttributeMap{
		Username: "uid", DisplayName: "cn", ExternalID: "entryUUID",
	}}}

	_, err := client.mapEntry(entry("uid=nobody,dc=example,dc=org", map[string][]byte{
		"uid": []byte("nobody"),
		"cn":  []byte("Nobody"),
	}))
	if !errors.Is(err, ErrNoExternalID) {
		t.Errorf("mapping an entry with no external id = %v, want ErrNoExternalID; "+
			"importing it would make the next rename a duplicate account", err)
	}
}

func TestEntryWithoutAUsernameIsRefused(t *testing.T) {
	client := &Client{cfg: Config{Attributes: AttributeMap{
		Username: "uid", DisplayName: "cn", ExternalID: "entryUUID",
	}}}

	_, err := client.mapEntry(entry("cn=Group,dc=example,dc=org", map[string][]byte{
		"cn":        []byte("A group, not a person"),
		"entryUUID": []byte("1f2e3d4c-5b6a-7988-9a0b-1c2d3e4f5061"),
	}))
	if !errors.Is(err, ErrNoUsername) {
		t.Errorf("mapping an entry with no username = %v, want ErrNoUsername", err)
	}
}

// A missing display name falls back to the username rather than dropping the
// account. Cosmetics must not cost somebody their access.
func TestMissingDisplayNameFallsBackToTheUsername(t *testing.T) {
	client := &Client{cfg: Config{Attributes: AttributeMap{
		Username: "uid", DisplayName: "displayName", ExternalID: "entryUUID",
	}}}

	mapped, err := client.mapEntry(entry("uid=terse,dc=example,dc=org", map[string][]byte{
		"uid":       []byte("terse"),
		"entryUUID": []byte("1f2e3d4c-5b6a-7988-9a0b-1c2d3e4f5062"),
	}))
	if err != nil {
		t.Fatalf("map entry: %v", err)
	}
	if mapped.DisplayName != "terse" {
		t.Errorf("displayName = %q, want the username as a fallback", mapped.DisplayName)
	}
}
