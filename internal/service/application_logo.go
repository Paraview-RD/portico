package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"net/http"
	"time"

	// Registered for their decoders, which is how the pixel bounds below are
	// read. The blank imports are the point: image.DecodeConfig knows only the
	// formats something has registered.
	_ "image/jpeg"
	_ "image/png"

	"github.com/google/uuid"

	"github.com/Paraview-RD/portico/internal/auth"
	"github.com/Paraview-RD/portico/internal/httpx"
	"github.com/Paraview-RD/portico/internal/store"
	"github.com/Paraview-RD/portico/internal/store/sqlcgen"
)

// Bounds on an uploaded logo.
//
// The byte cap is what a tile needs and nothing more: a 512×512 PNG of a
// company mark is a few tens of kilobytes, and the rows live in the same
// database as everything else, so this is also a bound on what a hostile
// administrator can put in a backup. The pixel cap exists because bytes are a
// poor proxy — a highly compressible image can be enormous when decoded, which
// is what a decompression bomb is.
const (
	MaxLogoBytes  = 512 << 10 // 512 KiB
	MaxLogoPixels = 1024
)

// Bounds on an uploaded branding background image.
//
// A background fills the whole sign-in screen rather than a small tile, so
// the logo's caps above would leave it visibly blurry — these are wider for
// the same reasons the logo's are what they are: the byte cap bounds what a
// hostile administrator can put in a backup, and the pixel cap catches a
// highly compressible decompression bomb that a byte cap alone would miss.
const (
	MaxBgImageBytes  = 4 << 20 // 4 MiB
	MaxBgImagePixels = 2560
)

// ErrUnsupportedImage is a file that will not be stored as a logo.
//
// One code for every rejection about the file's own content, with the specific
// reason in the message. A caller cannot act differently on "this is an SVG"
// than on "this is a text file that ends in .png", so splitting them would be
// codes nobody branches on.
var ErrUnsupportedImage = httpx.BadRequest("UNSUPPORTED_IMAGE",
	"A logo must be a PNG or JPEG image.")

// ErrLogoTooLarge is a file past the size or pixel bound.
var ErrLogoTooLarge = httpx.BadRequest("LOGO_TOO_LARGE",
	fmt.Sprintf("A logo must be under %d KiB and no more than %d pixels on a side.",
		MaxLogoBytes>>10, MaxLogoPixels))

// ErrBgImageTooLarge is a background image past the size or pixel bound.
var ErrBgImageTooLarge = httpx.BadRequest("BG_IMAGE_TOO_LARGE",
	fmt.Sprintf("A background image must be under %d MiB and no more than %d pixels on a side.",
		MaxBgImageBytes>>20, MaxBgImagePixels))

// ApplicationLogoService stores and serves the pictures on application tiles.
type ApplicationLogoService struct {
	store *store.Store
}

// NewApplicationLogoService wires the service.
//
// No audit dependency, unlike most services here. An upload is not yet a
// change to anything — the audited event is registering or editing the
// application that comes to reference it, which records who did it and when.
// A second entry for the upload would be an entry for an act with no effect.
func NewApplicationLogoService(st *store.Store) *ApplicationLogoService {
	return &ApplicationLogoService{store: st}
}

// Logo is a stored picture, ready to be written to a response.
type Logo struct {
	ID          string
	ContentType string
	Bytes       []byte
	// SHA256 is the ETag. Stored rather than computed on read: hashing the
	// body to answer a conditional request would defeat the point of one.
	SHA256 string
}

// detectLogoFormat decides what a file is from its content, and refuses
// anything that is not one of two raster formats.
//
// The uploader's filename and declared Content-Type are not consulted, at all.
// Both are chosen by whoever sends the request, so treating either as evidence
// would mean an SVG named .png is stored and later served as image/png — and a
// browser asked to render a document as an image it is not will sniff, at which
// point the extension has bought nothing and cost the check.
//
// **SVG is refused, and it is the reason this function exists.** An SVG is a
// document: it can carry script, and it would be served from this origin — the
// origin the administrative console is on. Rendered through <img> a browser
// will not run that script, which is why the existing code accepts an SVG
// *address* on a path an operator put there themselves. But a file uploaded
// through a web form and served back by this server can also be opened
// directly, in a tab, where it is a page with this origin's cookies. The safety
// of the <img> case is a property of one component's rendering, and a stored
// blob that is only safe because of how something happens to render it is a
// trap for whoever changes that component. See the same argument in
// normalizeLogoURI, which refuses data: URIs for the same reason.
//
// Sniffing with http.DetectContentType rather than by hand: it implements the
// WHATWG algorithm a browser uses, so what this stores and what a browser
// believes it received cannot disagree.
func detectLogoFormat(content []byte) (string, error) {
	switch http.DetectContentType(content) {
	case "image/png":
		return "image/png", nil
	case "image/jpeg":
		return "image/jpeg", nil
	default:
		return "", ErrUnsupportedImage
	}
}

// decodedBounds reports an image's pixel dimensions without decoding all of it.
//
// DecodeConfig reads the header only, so a file claiming to be enormous is
// refused before anything allocates for it. Both a check and a second opinion:
// a file that sniffs as a PNG and cannot be parsed as one is not a PNG,
// whatever the first bytes say.
func decodedBounds(content []byte) (width, height int, err error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return 0, 0, ErrUnsupportedImage
	}
	return config.Width, config.Height, nil
}

// Upload validates a file and stores it, returning the logo's id.
//
// The bytes are stored exactly as they arrived. Re-encoding them would be a way
// to guarantee the output is an image — and it would also silently change
// somebody's carefully made mark, lose an alpha channel or a colour profile,
// and turn every upload into a decode-encode cycle over untrusted input. What
// makes the stored file safe to serve is that it is one of two raster formats
// and is sent with the type it actually is.
func (s *ApplicationLogoService) Upload(
	ctx context.Context, actor auth.Principal, file io.Reader,
) (string, error) {
	return s.upload(ctx, actor, file, MaxLogoBytes, MaxLogoPixels, ErrLogoTooLarge)
}

// UploadBgImage validates and stores a branding background image, returning
// its id. Same table, same validation, same /t/<tenant>/logos/<id> serving
// path as Upload — a background image is not a distinct kind of row, only a
// different field ends up pointing at it. Only the size and pixel bounds
// differ, because a background fills the whole screen rather than a tile.
func (s *ApplicationLogoService) UploadBgImage(
	ctx context.Context, actor auth.Principal, file io.Reader,
) (string, error) {
	return s.upload(ctx, actor, file, MaxBgImageBytes, MaxBgImagePixels, ErrBgImageTooLarge)
}

func (s *ApplicationLogoService) upload(
	ctx context.Context, actor auth.Principal, file io.Reader,
	maxBytes, maxPixels int, tooLarge error,
) (string, error) {
	// One byte past the cap, so a file exactly at the limit is accepted and
	// anything larger is detected rather than silently truncated.
	content, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return "", fmt.Errorf("read upload: %w", err)
	}
	if len(content) > maxBytes {
		return "", tooLarge
	}
	if len(content) == 0 {
		return "", ErrUnsupportedImage
	}

	contentType, err := detectLogoFormat(content)
	if err != nil {
		return "", err
	}

	width, height, err := decodedBounds(content)
	if err != nil {
		return "", err
	}
	if width > maxPixels || height > maxPixels {
		return "", tooLarge
	}

	sum := sha256.Sum256(content)
	id := uuid.NewString()

	err = s.store.ForTenant(actor.TenantID).CreateApplicationLogo(ctx,
		sqlcgen.CreateApplicationLogoParams{
			ID:          id,
			ContentType: contentType,
			Bytes:       content,
			Sha256:      hex.EncodeToString(sum[:]),
			ByteSize:    int32(len(content)), // #nosec G115 -- bounded by maxBytes above
			CreatedAt:   store.Now(),
		})
	if err != nil {
		return "", fmt.Errorf("store upload: %w", err)
	}

	return id, nil
}

// Get returns a stored logo for serving.
func (s *ApplicationLogoService) Get(ctx context.Context, tenantID, id string) (Logo, error) {
	row, err := s.store.ForTenant(tenantID).GetApplicationLogo(ctx, id)
	if err != nil {
		if store.IsNoRows(err) {
			return Logo{}, httpx.NotFound("LOGO_NOT_FOUND", "No such logo.")
		}
		return Logo{}, fmt.Errorf("get logo: %w", err)
	}
	return Logo{
		ID:          row.ID,
		ContentType: row.ContentType,
		Bytes:       row.Bytes,
		SHA256:      row.Sha256,
	}, nil
}

// OrphanRetention is how long an upload survives without being referenced.
//
// An upload has to be stored before the form that would name it is saved, so a
// cancelled form leaves a row nobody points at. Long enough that somebody who
// uploaded a picture, went to lunch, and came back to finish the form still
// finds it there; short enough that abandoned uploads do not accumulate.
const OrphanRetention = 24 * time.Hour

// SweepOrphans deletes uploads that no application references and that are
// older than OrphanRetention. It reports how many it removed.
func (s *ApplicationLogoService) SweepOrphans(ctx context.Context, tenantID string, now time.Time) (int64, error) {
	removed, err := s.store.ForTenant(tenantID).
		DeleteOrphanedApplicationLogos(ctx, now.Add(-OrphanRetention))
	if err != nil {
		return 0, fmt.Errorf("sweep orphaned logos: %w", err)
	}
	return removed, nil
}

// ApplicationLogoPath is where an uploaded logo is served from.
//
// Built here rather than in the console so that one place decides how the
// address is spelled. It goes into the same logo_uri column that has accepted a
// path on this server since migration 00003, and takes the tenant-prefixed form
// the federation endpoints already use — because the row belongs to a tenant
// and the request that reads it has no principal to take one from.
//
// Deliberately not under /api. SecurityHeaders sets Cache-Control: no-store for
// that prefix, which is right for a payload carrying account data and exactly
// wrong for an immutable image fetched on every page load.
func ApplicationLogoPath(tenantCode, logoID string) string {
	return "/t/" + tenantCode + "/logos/" + logoID
}
