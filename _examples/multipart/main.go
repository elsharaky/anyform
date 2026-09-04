// Command multipart demonstrates the unified Marshal/Unmarshal API with the
// anyform.File type for file uploads. It builds a multipart body client-side
// and decodes it server-side without coupling to net/http.
package main

import (
	"fmt"

	"github.com/elsharaky/anyform"
)

type Upload struct {
	Title  string         `form:"title"`
	Avatar anyform.File   `form:"avatar"`
	Docs   []anyform.File `form:"documents"`
}

func main() {
	client := Upload{
		Title:  "My Submission",
		Avatar: anyform.File{Content: []byte("avatar-bytes"), ContentType: "image/png", Filename: "me.png"},
		Docs: []anyform.File{
			{Content: []byte("resume"), ContentType: "text/plain", Filename: "resume.txt"},
		},
	}

	// Client side: struct -> body + Content-Type. anyform auto-detects that the
	// struct contains File fields and produces multipart/form-data.
	body, contentType, err := anyform.Marshal(client)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Client multipart body (%d bytes), content-type: %s\n", len(body), contentType)

	// Server side: body + Content-Type -> struct. No net/http dependency and no
	// manual ParseMultipartForm call required.
	var server Upload
	if err := anyform.Unmarshal(body, contentType, &server); err != nil {
		panic(err)
	}

	fmt.Printf("Server title: %s\n", server.Title)
	fmt.Printf("Server avatar: %s (%s)\n", string(server.Avatar.Content), server.Avatar.Filename)
	for _, d := range server.Docs {
		fmt.Printf("Server doc: %s (%s)\n", string(d.Content), d.Filename)
	}
}
