package file

import (
	"mime"
	"mime/multipart"

	"github.com/kitwork/engine2/core/value"
)

// Upload is a file of the request's multipart body, request.file(tên) (mục 9). Its name, size and type are what
// the client said, never proof: name is the last name of what it sent, never a path to write to; type is the media
// type it claimed, never the content's. Its bytes stay where the multipart parse put them (memory for a small part,
// a file of the system past it, removed as the request ends) until write() copies them, never through the VM.
type Upload struct {
	field  string
	header *multipart.FileHeader
}

// NewUpload is the upload of the request's field, as its multipart parse holds it.
func NewUpload(field string, header *multipart.FileHeader) *Upload {
	return &Upload{field: field, header: header}
}

// RequestBound: an upload belongs to its request, whose end removes its bytes; a routine holds none.
func (u *Upload) RequestBound() {}

// Name is upload.name, the last name the client sent ("../../a.png" reads "a.png").
func (u *Upload) Name() value.Value { return value.String(u.header.Filename) }

// Size is upload.size, in bytes.
func (u *Upload) Size() value.Value { return value.Number(float64(u.header.Size)) }

// Type is upload.type, the media type the client claimed.
func (u *Upload) Type() value.Value { return value.String(u.header.Header.Get("Content-Type")) }

// MissingMember is any other word of an upload.
func (u *Upload) MissingMember(name string) value.Value {
	return value.Fail(value.TypeCode, "upload."+name+" không có: upload có name, size, type; lưu bằng file.open(\"uploads/\").segment(id, \"a.png\").write(upload) (KITWORK_FILE.md mục 9)")
}

// mediaTypeOf is the media type of an extension, ".pdf" → "application/pdf": none, or one without a type, is bytes
// (mục 4), never a type guessed from the content, which would serve an upload that looks like HTML as a page.
func mediaTypeOf(extension string) string {
	if extension != "" {
		if known := mime.TypeByExtension(extension); known != "" {
			return known
		}
	}
	return "application/octet-stream"
}
