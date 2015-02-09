package media_library

import (
	"bytes"
	"database/sql/driver"
	"errors"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"os"

	"github.com/disintegration/imaging"
)

var ErrNotImplemented = errors.New("not implemented")

type Base struct {
	Url        string
	Valid      bool
	FileName   string
	FileHeader *multipart.FileHeader
	CropOption *CropOption
	Reader     io.Reader
}

func (b *Base) Scan(value interface{}) error {
	switch v := value.(type) {
	case []*multipart.FileHeader:
		if len(v) > 0 {
			file := v[0]
			b.FileHeader, b.FileName, b.Valid = file, file.Filename, true
		}
	case []uint8:
		b.Url, b.Valid = string(v), true
	case string:
		b.Url, b.Valid = v, true
	default:
		fmt.Errorf("unsupported driver -> Scan pair for MediaLibrary")
	}
	return nil
}

func (b Base) Value() (driver.Value, error) {
	if b.Valid {
		return b.FileName, nil
	}
	return nil, nil
}

func (b Base) URL(...string) string {
	return b.Url
}

func (b Base) String() string {
	return b.URL()
}

func (b Base) GetFileName() string {
	return b.FileName
}

func (b Base) GetFileHeader() *multipart.FileHeader {
	return b.FileHeader
}

func (b Base) GetURLTemplate(option *Option) (path string) {
	if path = option.Get("url"); path == "" {
		path = "/system/{{class}}/{{primary_key}}/{{column}}/{{basename}}.{{nanotime}}.{{extension}}"
	}
	return
}

func (b *Base) SetCropOption(option *CropOption) {
	b.CropOption = option
}

func (b *Base) GetCropOption() *CropOption {
	return b.CropOption
}

func (b Base) Retrieve(url string) (*os.File, error) {
	return nil, ErrNotImplemented
}

func (b Base) Crop(ml MediaLibrary, option *Option) error {
	if file, err := ml.Retrieve(b.URL("original")); err == nil {
		if img, err := imaging.Decode(file); err == nil {
			var buffer bytes.Buffer
			var cropOption = b.CropOption
			rect := image.Rect(cropOption.X, cropOption.Y, cropOption.X+cropOption.Width, cropOption.Y+cropOption.Height)
			imaging.Crop(img, rect)
			imaging.Encode(&buffer, img, imaging.PNG)
			return ml.Store(b.URL(), option, &buffer)
		} else {
			return err
		}
	}
	return nil
}
