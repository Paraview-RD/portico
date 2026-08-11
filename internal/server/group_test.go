package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// Groups over SCIM. The membership operations are the point: they are what a
// directory actually runs, and the shapes below are the ones Okta and Entra
// send rather than the ones the RFC lists first.

func (c *scimClient) createGroup(t *testing.T, displayName, externalID string, memberIDs ...string) struct {
	ID string `json:"id"`
} {
	t.Helper()

	members := make([]map[string]string, 0, len(memberIDs))
	for _, id := range memberIDs {
		members = append(members, map[string]string{"value": id})
	}

	resp := c.do(t, http.MethodPost, "/Groups", map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
		"displayName": displayName,
		"externalId":  externalID,
		"members":     members,
	})
	if resp.Status != http.StatusCreated && resp.Status != http.StatusOK {
		t.Fatalf("create group %s: status %d, body %s", displayName, resp.Status, resp.Body)
	}

	var group struct {
		ID string `json:"id"`
	}
	resp.decode(t, &group)
	return group
}

// groupMembers reads back the membership as a client would.
func (c *scimClient) groupMembers(t *testing.T, groupID string) []string {
	t.Helper()
	resp := c.do(t, http.MethodGet, "/Groups/"+groupID, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("get group: %d %s", resp.Status, resp.Body)
	}

	var group struct {
		Members []struct {
			Value string `json:"value"`
		} `json:"members"`
	}
	resp.decode(t, &group)

	ids := make([]string, 0, len(group.Members))
	for _, member := range group.Members {
		ids = append(ids, member.Value)
	}
	return ids
}

func memberPatch(op, path string, value any) map[string]any {
	operation := map[string]any{"op": op}
	if path != "" {
		operation["path"] = path
	}
	if value != nil {
		operation["value"] = value
	}
	return map[string]any{
		"schemas":    []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
		"Operations": []map[string]any{operation},
	}
}

// A description is Portico's, and SCIM has no way to say it.
//
// The Group schema has displayName, externalId and members — no description
// — so a directory cannot send one and cannot mean to clear one. Every push
// nevertheless did: the handlers built a GroupInput without it, and the
// update wrote that empty field over whatever an administrator had typed.
//
// Three ways in, because they are three code paths and only one of them is
// the obvious PUT. A directory that re-creates a group it lost track of
// arrives at POST, and a rename arrives at PATCH.
func TestASCIMPushKeepsWhatSCIMCannotSay(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	client := newSCIMClient(t, api, "group-description")

	const description = "Who to page at three in the morning"

	create := func(name string) string {
		t.Helper()
		res := api.do(http.MethodPost, "/api/v1/groups", admin, map[string]any{
			"displayName": name, "description": description,
		})
		if res.Status != http.StatusOK {
			t.Fatalf("create %s: %d %s %s", name, res.Status, res.Code, res.Message)
		}
		var group struct {
			ID string `json:"id"`
		}
		res.into(t, &group)
		return group.ID
	}

	describedAs := func(id string) string {
		t.Helper()
		res := api.do(http.MethodGet, "/api/v1/groups/"+id, admin, nil)
		if res.Status != http.StatusOK {
			t.Fatalf("read group %s: %d %s", id, res.Status, res.Code)
		}
		var group struct {
			Description string `json:"description"`
		}
		res.into(t, &group)
		return group.Description
	}

	for _, push := range []struct {
		name string
		send func(id, displayName string)
	}{
		{"PUT", func(id, displayName string) {
			res := client.do(t, http.MethodPut, "/Groups/"+id, map[string]any{
				"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
				"displayName": displayName,
			})
			if res.Status != http.StatusOK {
				t.Fatalf("PUT: %d %s", res.Status, res.Body)
			}
		}},
		{"POST for a group that already exists", func(id, displayName string) {
			res := client.do(t, http.MethodPost, "/Groups", map[string]any{
				"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
				"displayName": displayName,
			})
			if res.Status != http.StatusOK && res.Status != http.StatusCreated {
				t.Fatalf("POST: %d %s", res.Status, res.Body)
			}
		}},
		{"PATCH that renames", func(id, displayName string) {
			res := client.do(t, http.MethodPatch, "/Groups/"+id, map[string]any{
				"schemas": []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
				"Operations": []map[string]any{
					{"op": "replace", "path": "displayName", "value": displayName + " Renamed"},
				},
			})
			if res.Status != http.StatusOK {
				t.Fatalf("PATCH: %d %s", res.Status, res.Body)
			}
		}},
	} {
		t.Run(push.name, func(t *testing.T) {
			name := "Support " + push.name
			id := create(name)

			push.send(id, name)

			if got := describedAs(id); got != description {
				t.Errorf("after a %s the description is %q, want %q; SCIM "+
					"cannot express this field, so a push cannot have meant "+
					"to clear it", push.name, got, description)
			}
		})
	}
}

func TestGroupsAreAdvertisedAndServed(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "groups-advertised")

	var listing struct {
		Resources []struct {
			Name     string `json:"name"`
			Endpoint string `json:"endpoint"`
		} `json:"Resources"`
	}
	client.do(t, http.MethodGet, "/ResourceTypes", nil).decode(t, &listing)

	var found bool
	for _, rt := range listing.Resources {
		if rt.Name == "Group" {
			found = true
		}
	}
	if !found {
		t.Fatal("Group is not advertised in /ResourceTypes")
	}

	if resp := client.do(t, http.MethodGet, "/Groups", nil); resp.Status != http.StatusOK {
		t.Errorf("GET /Groups returned %d", resp.Status)
	}
}

func TestAddingAndRemovingAMemberChangesTheGroup(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "membership")

	user := client.createUser(t, "member-one", "ext-member-one")
	group := client.createGroup(t, "Engineering", "ext-engineering")

	// The shape Okta sends to add.
	resp := client.do(t, http.MethodPatch, "/Groups/"+group.ID,
		memberPatch("add", "members", []map[string]string{{"value": user.ID}}))
	if resp.Status != http.StatusOK {
		t.Fatalf("add member: %d %s", resp.Status, resp.Body)
	}

	members := client.groupMembers(t, group.ID)
	if len(members) != 1 || members[0] != user.ID {
		t.Fatalf("after add, members = %v, want [%s]", members, user.ID)
	}

	// And the shape it sends to remove: the id inside a filter in the path,
	// which is the form a naive path lookup mangles.
	resp = client.do(t, http.MethodPatch, "/Groups/"+group.ID,
		memberPatch("remove", fmt.Sprintf("members[value eq %q]", user.ID), nil))
	if resp.Status != http.StatusOK {
		t.Fatalf("remove member: %d %s", resp.Status, resp.Body)
	}

	if members := client.groupMembers(t, group.ID); len(members) != 0 {
		t.Errorf("after remove, members = %v, want none", members)
	}
}

func TestRemovingAMemberByValueInTheBodyAlsoWorks(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "membership-body")

	user := client.createUser(t, "member-two", "ext-member-two")
	group := client.createGroup(t, "Support", "ext-support", user.ID)

	// The other remove shape, with the member in the value rather than the
	// path. Both are in the wild and a server that handles one is broken for
	// half its integrators.
	resp := client.do(t, http.MethodPatch, "/Groups/"+group.ID,
		memberPatch("remove", "members", []map[string]string{{"value": user.ID}}))
	if resp.Status != http.StatusOK {
		t.Fatalf("remove member: %d %s", resp.Status, resp.Body)
	}

	if members := client.groupMembers(t, group.ID); len(members) != 0 {
		t.Errorf("members = %v, want none", members)
	}
}

func TestReplacingMembersSetsExactlyThatList(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "membership-replace")

	first := client.createUser(t, "replace-one", "ext-replace-one")
	second := client.createUser(t, "replace-two", "ext-replace-two")
	group := client.createGroup(t, "Finance", "ext-finance", first.ID)

	// How Entra reconciles: hand over the whole membership.
	resp := client.do(t, http.MethodPatch, "/Groups/"+group.ID,
		memberPatch("replace", "members", []map[string]string{{"value": second.ID}}))
	if resp.Status != http.StatusOK {
		t.Fatalf("replace members: %d %s", resp.Status, resp.Body)
	}

	members := client.groupMembers(t, group.ID)
	if len(members) != 1 || members[0] != second.ID {
		t.Errorf("members = %v, want exactly [%s]: replace means the whole set",
			members, second.ID)
	}
}

func TestAMemberThatDoesNotExistIsRefusedNotSkipped(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "membership-unknown")

	group := client.createGroup(t, "Nowhere", "ext-nowhere")

	resp := client.do(t, http.MethodPatch, "/Groups/"+group.ID,
		memberPatch("add", "members",
			[]map[string]string{{"value": "00000000-0000-0000-0000-000000000000"}}))

	// Skipping it silently would leave a group that looks synchronized and is
	// not — the one failure a directory cannot detect from its own side.
	if resp.Status != http.StatusBadRequest {
		t.Errorf("adding an unknown member returned %d, want 400", resp.Status)
	}
	if got := resp.scimType(t); got != "invalidValue" {
		t.Errorf("scimType = %q, want invalidValue", got)
	}
}

func TestAGroupIsReconciledOnExternalIDNotDuplicated(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "group-reconcile")

	client.createGroup(t, "Original Name", "ext-reconcile-group")

	// Renamed in the directory and pushed again. Same reasoning as for
	// users: the externalId is the only key that survives a rename, and
	// without matching on it the directory ends up duplicated.
	client.createGroup(t, "Renamed Group", "ext-reconcile-group")

	var listing struct {
		TotalResults int `json:"totalResults"`
		Resources    []struct {
			DisplayName string `json:"displayName"`
		} `json:"Resources"`
	}
	client.do(t, http.MethodGet,
		`/Groups?filter=`+url.QueryEscape(`externalId eq "ext-reconcile-group"`), nil).
		decode(t, &listing)

	if listing.TotalResults != 1 {
		t.Fatalf("externalId matches %d groups, want 1", listing.TotalResults)
	}
	if listing.Resources[0].DisplayName != "Renamed Group" {
		t.Errorf("displayName = %q; the rename was not applied",
			listing.Resources[0].DisplayName)
	}
}

func TestAUserResourceReportsItsGroups(t *testing.T) {
	api := newAPITest(t)
	client := newSCIMClient(t, api, "user-groups")

	user := client.createUser(t, "grouped-user", "ext-grouped-user")
	group := client.createGroup(t, "Readers", "ext-readers", user.ID)

	// A client reads the user back to confirm a push landed.
	resp := client.do(t, http.MethodGet, "/Users/"+user.ID, nil)
	var got struct {
		Groups []struct {
			Value string `json:"value"`
		} `json:"groups"`
	}
	resp.decode(t, &got)

	if len(got.Groups) != 1 || got.Groups[0].Value != group.ID {
		t.Errorf("user's groups = %+v, want the group it was added to", got.Groups)
	}
}

func TestGroupsAndOrganizationsAreSeparate(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()
	client := newSCIMClient(t, api, "separate")

	// A group is not an organization. Creating one must not appear in the
	// org chart, which single-membership and downstream code depend on.
	client.createGroup(t, "Not An Org", "ext-not-an-org")

	orgs := api.do(http.MethodGet, "/api/v1/organizations", admin, nil)
	var organizations []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(orgs.Data, &organizations); err != nil {
		t.Fatalf("decode organizations: %v", err)
	}
	for _, org := range organizations {
		if org.Name == "Not An Org" {
			t.Fatal("a SCIM group became an organization. They are different " +
				"shapes — one membership versus many — and conflating them " +
				"breaks the org chart to serve the other concept.")
		}
	}
}
