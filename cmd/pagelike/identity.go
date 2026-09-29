package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/site"
)

// End-user identity administration: local accounts, identity links for
// migrated subjects, and each site's identity provider settings
// (docs/identity.md).

const identityUsage = `  pagelike user     add --site NAME USER [--email E] [--verified] [--name N] [--roles a,b] [--password P | --password-stdin]
  pagelike user     passwd --site NAME USER [--password-stdin | --clear]
  pagelike user     list --site NAME | delete --site NAME USER
  pagelike user     link --site NAME --issuer URL --idp-sub SUB USER | unlink --site NAME --issuer URL --idp-sub SUB | links --site NAME
  pagelike identity show --site NAME
  pagelike identity set --site NAME [--issuer URL] [--client-id ID] [--client-secret-stdin] [--scopes "openid email profile"]
                    [--roles-claim roles] [--login-path P] [--logout-path P] [--callback-path P]
                    [--session-lifetime 720h] [--session-cookie NAME] [--cookies lax|partitioned]
                    [--embed-origins https://a.example,https://b.example | none]
  pagelike identity clear-oidc --site NAME
`

// readSecret reads one line from stdin (passwords, client secrets).
func readSecret() (string, error) {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("reading stdin: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func openSite(ctx context.Context, data, name string) (*site.Registry, *site.Site, error) {
	reg, err := openRegistry(data)
	if err != nil {
		return nil, nil, err
	}
	st, err := reg.Get(ctx, name)
	if err != nil {
		reg.Close()
		return nil, nil, fmt.Errorf("site %q: %w", name, err)
	}
	return reg, st, nil
}

func cmdUser(args []string) error {
	if len(args) < 1 {
		return errors.New("user: expected add|passwd|list|delete|link|unlink|links")
	}
	fs := flag.NewFlagSet("user", flag.ExitOnError)
	data := fs.String("data", defaultData(), "data directory")
	siteName := fs.String("site", "", "site")
	email := fs.String("email", "", "email")
	verified := fs.Bool("verified", false, "email is verified")
	name := fs.String("name", "", "display name")
	roles := fs.String("roles", "", "comma-separated roles")
	password := fs.String("password", "", "password (prefer --password-stdin)")
	pwStdin := fs.Bool("password-stdin", false, "read password from stdin")
	clear := fs.Bool("clear", false, "passwd: disable password sign-in")
	issuer := fs.String("issuer", "", "link: OpenID issuer (or its openid-configuration URL)")
	idpSub := fs.String("idp-sub", "", "link: the provider's subject for the user")
	sub := args[0]
	fs.Parse(reorder(args[1:]))
	ctx := context.Background()
	reg, st, err := openSite(ctx, *data, *siteName)
	if err != nil {
		return err
	}
	defer reg.Close()
	pw := *password
	if *pwStdin {
		if pw, err = readSecret(); err != nil {
			return err
		}
	}
	switch sub {
	case "add":
		if fs.NArg() != 1 {
			return errors.New("user add --site NAME USER")
		}
		return st.Users.Upsert(ctx, identity.User{Sub: fs.Arg(0), Email: *email, EmailVerified: *verified, Name: *name, Roles: identity.SplitRoles(*roles)}, pw)
	case "passwd":
		if fs.NArg() != 1 {
			return errors.New("user passwd --site NAME USER [--password-stdin | --clear]")
		}
		usr, err := st.Users.Get(ctx, fs.Arg(0))
		if err != nil {
			return fmt.Errorf("user %q: %w", fs.Arg(0), err)
		}
		if *clear {
			return st.Users.ClearPassword(ctx, usr.Sub)
		}
		if pw == "" {
			return errors.New("user passwd: give --password-stdin (or --clear)")
		}
		return st.Users.Upsert(ctx, *usr, pw)
	case "list":
		us, err := st.Users.List(ctx)
		if err != nil {
			return err
		}
		for _, u := range us {
			fmt.Printf("%s\t%s\tverified=%v\tpassword=%v\t%s\t%s\n", u.Sub, u.Email, u.EmailVerified, u.HasPassword, u.Name, strings.Join(u.Roles, ","))
		}
	case "delete":
		if fs.NArg() != 1 {
			return errors.New("user delete --site NAME USER")
		}
		return st.Users.Delete(ctx, fs.Arg(0))
	case "link":
		if fs.NArg() != 1 || *issuer == "" || *idpSub == "" {
			return errors.New("user link --site NAME --issuer URL --idp-sub SUB USER")
		}
		return st.Users.SetLink(ctx, identity.Link{Issuer: *issuer, IdPSub: *idpSub, Sub: fs.Arg(0)})
	case "unlink":
		if *issuer == "" || *idpSub == "" {
			return errors.New("user unlink --site NAME --issuer URL --idp-sub SUB")
		}
		return st.Users.DeleteLink(ctx, *issuer, *idpSub)
	case "links":
		ls, err := st.Users.Links(ctx)
		if err != nil {
			return err
		}
		for _, l := range ls {
			fmt.Printf("%s\t%s\t→ %s\n", l.Issuer, l.IdPSub, l.Sub)
		}
	default:
		return fmt.Errorf("user: unknown subcommand %q", sub)
	}
	return nil
}

func cmdIdentity(args []string) error {
	if len(args) < 1 {
		return errors.New("identity: expected show|set|clear-oidc")
	}
	fs := flag.NewFlagSet("identity", flag.ExitOnError)
	data := fs.String("data", defaultData(), "data directory")
	siteName := fs.String("site", "", "site")
	issuer := fs.String("issuer", "", "OpenID provider: openid-configuration URL or issuer URL")
	clientID := fs.String("client-id", "", "OpenID client id")
	secretStdin := fs.Bool("client-secret-stdin", false, "read the OpenID client secret from stdin")
	scopes := fs.String("scopes", "", `requested scopes (default "openid email profile")`)
	rolesClaim := fs.String("roles-claim", "", `claim carrying roles (default "roles"; dotted path allowed)`)
	loginPath := fs.String("login-path", "", "login path (default /auth/login)")
	logoutPath := fs.String("logout-path", "", "logout path (default /auth/logout)")
	callbackPath := fs.String("callback-path", "", "callback path (default /auth/callback)")
	lifetime := fs.String("session-lifetime", "", "signed-in session lifetime (Go duration, default 720h)")
	cookieName := fs.String("session-cookie", "", "session cookie name (default __Host-session)")
	cookies := fs.String("cookies", "", "cookie policy: lax (default) or partitioned")
	embed := fs.String("embed-origins", "", `origins allowed to frame the sign-in page, comma-separated ("none" clears)`)
	sub := args[0]
	fs.Parse(reorder(args[1:]))
	ctx := context.Background()
	reg, st, err := openSite(ctx, *data, *siteName)
	if err != nil {
		return err
	}
	defer reg.Close()
	switch sub {
	case "show":
		s := st.Settings()
		fmt.Printf("login path\t%s\nlogout path\t%s\ncallback path\t%s\n", s.LoginPath, s.LogoutPath, s.CallbackPath)
		fmt.Printf("session lifetime\t%s\nsession cookie\t%s\ncookie policy\t%s\n", or(s.SessionLifetime, identity.SessionTTL.String()), or(s.SessionCookie, identity.CookieSecure), s.Cookies)
		if s.OIDC == nil {
			fmt.Println("openid provider\tnone")
		} else {
			secret := "unset"
			if s.OIDC.ClientSecret != "" {
				secret = "set"
			}
			fmt.Printf("openid provider\t%s\nclient id\t%s\nclient secret\t%s\nscopes\t%s\nroles claim\t%s\n",
				s.OIDC.Issuer, s.OIDC.ClientID, secret, or(s.OIDC.Scopes, strings.Join(identity.DefaultScopes, " ")), or(s.OIDC.RolesClaim, "roles"))
		}
		us, _ := st.Users.List(ctx)
		n := 0
		for _, u := range us {
			if u.HasPassword {
				n++
			}
		}
		fmt.Printf("local accounts\t%d (%d with a password)\n", len(us), n)
	case "set":
		secret := ""
		if *secretStdin {
			if secret, err = readSecret(); err != nil {
				return err
			}
		}
		for _, p := range []string{*loginPath, *logoutPath, *callbackPath} {
			if p != "" && (!strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//")) {
				return fmt.Errorf("identity set: %q is not an absolute path", p)
			}
		}
		if *cookies != "" && *cookies != "lax" && *cookies != "partitioned" {
			return errors.New("identity set: --cookies must be lax or partitioned")
		}
		if *lifetime != "" {
			if d, err := time.ParseDuration(*lifetime); err != nil || d <= 0 {
				return fmt.Errorf("identity set: bad --session-lifetime %q", *lifetime)
			}
		}
		return st.UpdateSettings(ctx, func(s *site.Settings) {
			if *issuer != "" || *clientID != "" || *secretStdin || *scopes != "" || *rolesClaim != "" {
				if s.OIDC == nil {
					s.OIDC = &site.OIDCConfig{}
				}
				setIf(&s.OIDC.Issuer, *issuer)
				setIf(&s.OIDC.ClientID, *clientID)
				setIf(&s.OIDC.ClientSecret, secret)
				setIf(&s.OIDC.Scopes, *scopes)
				setIf(&s.OIDC.RolesClaim, *rolesClaim)
			}
			setIf(&s.LoginPath, *loginPath)
			setIf(&s.LogoutPath, *logoutPath)
			setIf(&s.CallbackPath, *callbackPath)
			setIf(&s.SessionLifetime, *lifetime)
			setIf(&s.SessionCookie, *cookieName)
			setIf(&s.Cookies, *cookies)
			switch *embed {
			case "":
			case "none":
				s.EmbedOrigins = nil
			default:
				s.EmbedOrigins = strings.Split(*embed, ",")
			}
		})
	case "clear-oidc":
		return st.UpdateSettings(ctx, func(s *site.Settings) { s.OIDC = nil })
	default:
		return fmt.Errorf("identity: unknown subcommand %q", sub)
	}
	return nil
}

func setIf(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
