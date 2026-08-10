package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	casPrefix       = "/cas/"
	casLoginPath    = "/cas"
	casCallbackPath = "/cas/callback"
)

type casParty struct {
	// portico is held parsed rather than as a string so that every endpoint
	// below is built by setting a path and a query on it. A ticket arrives
	// from the browser, and string concatenation is how a value like that
	// ends up deciding which host gets contacted.
	portico *url.URL

	// service is the URL Portico sends the browser back to, and it is held
	// as one string rather than rebuilt at each step on purpose: CAS binds a
	// ticket to the service it was issued for, and validation compares the
	// two parameters byte for byte. Two spellings of the same address are
	// two services.
	service string
	client  *http.Client
}

func newCAS(portico, base string) (*casParty, error) {
	root, err := url.Parse(portico)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", portico, err)
	}
	return &casParty{
		portico: root,
		service: base + casCallbackPath,
		client:  &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// endpoint builds one of Portico's CAS URLs.
func (c *casParty) endpoint(path string, query url.Values) string {
	target := *c.portico
	target.Path = strings.TrimSuffix(target.Path, "/") + path
	target.RawQuery = query.Encode()
	return target.String()
}

func (c *casParty) mount(mux *http.ServeMux) {
	mux.HandleFunc(casLoginPath, c.begin)
	mux.HandleFunc(casCallbackPath, c.consume)
}

// begin sends the browser to Portico's CAS login endpoint.
func (c *casParty) begin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r,
		c.endpoint("/cas/login", url.Values{"service": {c.service}}),
		http.StatusFound)
}

// consume exchanges the ticket for the person's identity.
//
// This is the hop that makes CAS different from the other two: the browser
// carries only an opaque ticket, and the application turns it into an
// identity over a connection of its own, back to a server it was configured
// with. Nothing the browser holds is worth anything to anybody else.
func (c *casParty) consume(w http.ResponseWriter, r *http.Request) {
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" {
		page(w, http.StatusBadRequest, "caserror", map[string]string{
			"Stage":  "reading the response",
			"Detail": "Portico sent the browser back without a ticket parameter.",
		})
		return
	}

	// p3, not the CAS 2.0 endpoint: attributes are a CAS 3.0 addition, and
	// they are most of what there is to look at here.
	validate := c.endpoint("/cas/p3/serviceValidate", url.Values{
		"service": {c.service},
		"ticket":  {ticket},
	})

	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, validate, nil)
	if err != nil {
		page(w, http.StatusInternalServerError, "caserror", map[string]string{
			"Stage": "building the validation request", "Detail": err.Error(),
		})
		return
	}
	response, err := c.client.Do(request)
	if err != nil {
		page(w, http.StatusBadGateway, "caserror", map[string]string{
			"Stage": "validating the ticket", "Detail": err.Error(),
		})
		return
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		page(w, http.StatusBadGateway, "caserror", map[string]string{
			"Stage": "reading the validation response", "Detail": err.Error(),
		})
		return
	}

	var envelope casServiceResponse
	if err := xml.Unmarshal(body, &envelope); err != nil {
		page(w, http.StatusBadGateway, "caserror", map[string]string{
			"Stage": "parsing the validation response", "Detail": err.Error(),
		})
		return
	}

	// A failure comes back as 200 with a failure element inside, which the
	// specification requires and several clients get wrong by treating the
	// status code as the answer. Reading the body is the only way to know.
	if envelope.Failure != nil {
		page(w, http.StatusUnauthorized, "caserror", map[string]string{
			"Stage": "validating the ticket",
			"Detail": fmt.Sprintf("%s — %s",
				envelope.Failure.Code, strings.TrimSpace(envelope.Failure.Message)),
		})
		return
	}
	if envelope.Success == nil {
		page(w, http.StatusBadGateway, "caserror", map[string]string{
			"Stage":  "validating the ticket",
			"Detail": "the response carried neither a success nor a failure element",
		})
		return
	}

	page(w, http.StatusOK, "cassignedin", casView(envelope.Success, ticket))
}

// casServiceResponse is the CAS 3.0 validation document.
//
// Only the parts this page shows are modelled. Attributes are captured as
// raw XML and walked below, because CAS puts each attribute in an element
// named after itself — a shape Go's XML decoder has no way to express as
// struct fields.
type casServiceResponse struct {
	XMLName xml.Name        `xml:"serviceResponse"`
	Success *casSuccess     `xml:"authenticationSuccess"`
	Failure *casFailureBody `xml:"authenticationFailure"`
}

type casSuccess struct {
	User       string `xml:"user"`
	Attributes struct {
		Inner []byte `xml:",innerxml"`
	} `xml:"attributes"`
}

type casFailureBody struct {
	Code    string `xml:"code,attr"`
	Message string `xml:",chardata"`
}

type casSignedInView struct {
	User       string
	Ticket     string
	Attributes []casAttribute
}

type casAttribute struct {
	Name  string
	Value string
}

func casView(success *casSuccess, ticket string) casSignedInView {
	view := casSignedInView{User: success.User, Ticket: ticket}

	decoder := xml.NewDecoder(bytes.NewReader(success.Attributes.Inner))
	var name string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch element := token.(type) {
		case xml.StartElement:
			name = element.Name.Local
		case xml.CharData:
			if value := strings.TrimSpace(string(element)); value != "" && name != "" {
				view.Attributes = append(view.Attributes,
					casAttribute{Name: name, Value: value})
			}
		case xml.EndElement:
			name = ""
		}
	}
	sort.Slice(view.Attributes, func(i, j int) bool {
		return view.Attributes[i].Name < view.Attributes[j].Name
	})
	return view
}
