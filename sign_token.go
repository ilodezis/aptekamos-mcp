package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/ilodezis/aptekamos-mcp/internal/oauth"
)

// signTokenCmd implements `aptekamos-mcp sign-token [--days N]`: it prints a bearer token signed with
// TOKEN_SECRET (and CLIENT_ID, default aptekamos-claude-connector) for clients that cannot run the OAuth flow.
func signTokenCmd(args []string, getenv func(string) string, out io.Writer) error {
	fs := flag.NewFlagSet("sign-token", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	days := fs.Int("days", 365, "token lifetime in days")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if *days <= 0 {
		return fmt.Errorf("--days must be positive")
	}
	secret := getenv("TOKEN_SECRET")
	if secret == "" {
		return fmt.Errorf("TOKEN_SECRET is not set")
	}
	clientID := getenv("CLIENT_ID")
	if clientID == "" {
		clientID = oauth.DefaultClientID
	}
	_, err := fmt.Fprintln(out, oauth.Sign(secret, clientID, int64(*days)*24*3600))
	return err
}
