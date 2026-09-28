package hotspot

import (
	"fmt"
	"io"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// wifiPayload builds the standard "WIFI:" QR payload, escaping reserved chars.
func wifiPayload(ssid, pass string, open, hidden bool) string {
	var b strings.Builder
	b.WriteString("WIFI:")
	if open {
		b.WriteString("T:nopass;")
	} else {
		b.WriteString("T:WPA;")
	}
	b.WriteString("S:" + escapeWifi(ssid) + ";")
	if !open {
		b.WriteString("P:" + escapeWifi(pass) + ";")
	}
	if hidden {
		b.WriteString("H:true;")
	} else {
		b.WriteString("H:false;")
	}
	b.WriteString(";")
	return b.String()
}

func escapeWifi(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		";", `\;`,
		",", `\,`,
		":", `\:`,
		`"`, `\"`,
	).Replace(s)
}

// renderQR writes the QR for payload to w. invert flips colors for dark themes.
func renderQR(w io.Writer, payload string, invert bool) error {
	q, err := qrcode.New(payload, qrcode.Medium)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, q.ToSmallString(invert))
	return err
}

// writeQRPNG saves the QR as a PNG image of the given pixel size.
func writeQRPNG(path, payload string, size int) error {
	return qrcode.WriteFile(payload, qrcode.Medium, size, path)
}
