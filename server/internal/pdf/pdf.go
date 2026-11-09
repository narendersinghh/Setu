// Package pdf renders HTML to PDF with Gotenberg (headless Chromium sidecar).
package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// Asset is an extra file referenced from the HTML by its name (images, fonts).
type Asset struct {
	Name string
	Data []byte
}

type Options struct {
	PaperWidth, PaperHeight float64 // inches (default A4 8.27×11.7)
	Margin                  float64 // inches (default 0)
	Landscape               bool
}

// Render converts a full HTML document to PDF bytes.
func Render(ctx context.Context, gotenbergURL, html string, assets []Asset, o Options) ([]byte, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, _ := w.CreateFormFile("files", "index.html")
	io.WriteString(fw, html)
	for _, a := range assets {
		fw, _ := w.CreateFormFile("files", a.Name)
		fw.Write(a.Data)
	}
	if o.PaperWidth == 0 {
		o.PaperWidth, o.PaperHeight = 8.27, 11.7
	}
	for k, v := range map[string]string{
		"paperWidth": fmt.Sprint(o.PaperWidth), "paperHeight": fmt.Sprint(o.PaperHeight),
		"marginTop": fmt.Sprint(o.Margin), "marginBottom": fmt.Sprint(o.Margin), "marginLeft": fmt.Sprint(o.Margin), "marginRight": fmt.Sprint(o.Margin),
		"printBackground": "true", "preferCssPageSize": "true", "landscape": fmt.Sprint(o.Landscape),
	} {
		w.WriteField(k, v)
	}
	w.Close()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gotenbergURL+"/forms/chromium/convert/html", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pdf engine unreachable: %w", err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 100<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("pdf engine: HTTP %d: %s", res.StatusCode, string(b[:min(len(b), 300)]))
	}
	return b, nil
}
