// parse
package mltpart

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
)

func ParseMultipartFormEx(r *http.Request, maxMemory int64) (f *Form, err error) {
	if r.MultipartForm != nil {
		return nil, errors.New("Для разбора запроса необходимо использовать ReadFormEx")
	}

	if r.Form == nil {
		err := r.ParseForm()
		if err != nil {
			return nil, err
		}
	}

	mr, err := r.MultipartReader()
	if err != nil {
		return nil, err
	}

	f, err = ReadFormEx(mr, maxMemory)
	if err != nil {
		return nil, err
	}
	for k, v := range f.Value {
		r.Form[k] = append(r.Form[k], v...)
		r.PostForm[k] = append(r.PostForm[k], v...)
	}

	return f, nil
}

// TODO(adg,bradfitz): find a way to unify the DoS-prevention strategy here
// with that of the http package's ParseForm.

// ReadFormEx parses an entire multipart message whose parts have
// a Content-Disposition of "form-data".
// It stores up to maxMemory bytes of the file parts in memory
// and the remainder on disk in temporary files.
func ReadFormEx(r *multipart.Reader, maxMemory int64) (f *Form, err error) {
	lastArgNames := ""
	form := &Form{make(map[string][]string), make(map[string][]*FileHeader)}
	defer func() {
		if err != nil {
			form.RemoveAll()
		}
	}()

	maxValueBytes := int64(10 << 20) // 10 MB is a lot of text.
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		name := p.FormName()
		if name == "" {
			continue
		}
		filename := p.FileName()

		var b bytes.Buffer

		if filename == "" {
			// value, store as string in memory
			n, err := io.CopyN(&b, p, maxValueBytes)
			if err != nil && err != io.EOF {
				return nil, err
			}
			maxValueBytes -= n
			if maxValueBytes == 0 {
				return nil, errors.New("multipart: message too large")
			}
			// Если существует файл с именем параметра name,
			// то текущее значение добавляем как еще один файл
			if _, ok := form.File[name]; ok {
				fh := &FileHeader{
					Filename: b.String(),
					Header:   nil,
					LastArg:  "",
				}
				form.File[name] = append(form.File[name], fh)
			} else {
				form.Value[name] = append(form.Value[name], b.String())
			}
			if name == "p_arg_names" {
				lastArgNames = b.String()
			}
			continue
		}

		// file, store in memory or on disk
		fh := &FileHeader{
			Filename: filename,
			Header:   p.Header,
			LastArg:  lastArgNames,
		}
		n, err := io.CopyN(&b, p, maxMemory+1)
		if err != nil && err != io.EOF {
			return nil, err
		}
		if n > maxMemory {
			// too big, write to disk and flush buffer
			file, err := ioutil.TempFile("", "multipart-")
			if err != nil {
				return nil, err
			}
			defer file.Close()
			_, err = io.Copy(file, io.MultiReader(&b, p))
			if err != nil {
				os.Remove(file.Name())
				return nil, err
			}
			fh.tmpfile = file.Name()
		} else {
			fh.content = b.Bytes()
			maxMemory -= n
		}
		// Проверяе, были ли занчения параметра с именем name  добавлены как строковые
		// Если были, то превращаем их в заголовки файлов
		if _, ok := form.Value[name]; ok {
			for _, v := range form.Value[name] {
				form.File[name] = append(form.File[name], &FileHeader{
					Filename: v,
					Header:   nil,
					LastArg:  "",
				})
			}
			delete(form.Value, name)
		}
		form.File[name] = append(form.File[name], fh)
	}

	return form, nil
}

// Form is a parsed multipart form.
// Its File parts are stored either in memory or on disk,
// and are accessible via the *FileHeader's Open method.
// Its Value parts are stored as strings.
// Both are keyed by field name.
type Form struct {
	Value map[string][]string
	File  map[string][]*FileHeader
}

// RemoveAll removes any temporary files associated with a Form.
func (f *Form) RemoveAll() error {
	var err error
	for _, fhs := range f.File {
		for _, fh := range fhs {
			if fh.tmpfile != "" {
				e := os.Remove(fh.tmpfile)
				if e != nil && err == nil {
					err = e
				}
			}
		}
	}
	return err
}

// A FileHeader describes a file part of a multipart request.
type FileHeader struct {
	Filename string
	Header   textproto.MIMEHeader

	content []byte
	tmpfile string
	LastArg string
}

// Open opens and returns the FileHeader's associated File.
func (fh *FileHeader) Open() (File, string, error) {
	if b := fh.content; b != nil {
		r := io.NewSectionReader(bytes.NewReader(b), 0, int64(len(b)))
		return sectionReadCloser{r}, "", nil
	}
	fl, err := os.Open(fh.tmpfile)
	return fl,  fh.tmpfile, err
}

// File is an interface to access the file part of a multipart message.
// Its contents may be either stored in memory or on disk.
// If stored on disk, the File's underlying concrete type will be an *os.File.
type File interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
}

// helper types to turn a []byte into a File

type sectionReadCloser struct {
	*io.SectionReader
}

func (rc sectionReadCloser) Close() error {
	return nil
}
