// Package deps pins third-party libraries shared by modules so parallel work never edits go.mod.
package deps

import (
	_ "github.com/PuerkitoBio/goquery"
	_ "github.com/emersion/go-imap/client"
	_ "github.com/emersion/go-message/mail"
	_ "golang.org/x/net/html"
)
