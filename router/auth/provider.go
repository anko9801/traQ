package auth

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"strconv"
	"time"

	"github.com/disintegration/imaging"
	vd "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"
	"github.com/traPtitech/traQ/model"
	"github.com/traPtitech/traQ/repository"
	"github.com/traPtitech/traQ/router/consts"
	"github.com/traPtitech/traQ/router/extension/herror"
	"github.com/traPtitech/traQ/router/session"
	"github.com/traPtitech/traQ/service/file"
	"github.com/traPtitech/traQ/service/rbac/role"
	"github.com/traPtitech/traQ/utils/random"
	"github.com/traPtitech/traQ/utils/validator"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

const (
	cookieName         = "traq_ext_auth_cookie"
	cookieMaxAge       = 60 * 5
	accountLinkingFlag = "__account_linking"
)

type Provider interface {
	FetchUserInfo(t *oauth2.Token) (UserInfo, error)
	LoginHandler(c echo.Context) error
	CallbackHandler(c echo.Context) error
	L() *zap.Logger
}

type UserInfo interface {
	GetProviderName() string
	GetID() string
	GetRawName() string
	GetName() string
	GetDisplayName() string
	GetProfileImage() ([]byte, error)
	IsLoginAllowedUser() bool
}

func defaultLoginHandler(sessStore session.Store, oac *oauth2.Config) echo.HandlerFunc {
	return func(c echo.Context) error {
		if len(c.Request().Header.Get(echo.HeaderAuthorization)) > 0 {
			return herror.BadRequest("Authorization Header must not be set.")
		}

		sess, err := sessStore.GetSession(c, false)
		if err != nil {
			return herror.InternalServerError(err)
		}

		if isTrue(c.QueryParam("link")) {
			// アカウント関連付けモード
			if sess == nil || sess.UserID() == uuid.Nil {
				return herror.Unauthorized("You are not logged in. Please login.")
			}
			if err := sess.Set(accountLinkingFlag, true); err != nil {
				return herror.InternalServerError(err)
			}
		} else {
			// ログインモード
			if sess != nil && sess.UserID() != uuid.Nil {
				return herror.BadRequest("You have already logged in. Please logout once.")
			}
		}

		state := random.SecureAlphaNumeric(32)
		c.SetCookie(&http.Cookie{
			Name:     cookieName,
			Value:    state,
			Path:     "/",
			Expires:  time.Now().Add(cookieMaxAge * time.Second),
			MaxAge:   cookieMaxAge,
			HttpOnly: true,
		})
		return c.Redirect(http.StatusFound, oac.AuthCodeURL(state))
	}
}

func defaultCallbackHandler(p Provider, oac *oauth2.Config, repo repository.Repository, fm file.Manager, sessStore session.Store, allowSignUp bool) echo.HandlerFunc {
	return func(c echo.Context) error {
		if len(c.Request().Header.Get(echo.HeaderAuthorization)) > 0 {
			return herror.BadRequest("Authorization Header must not be set.")
		}

		code := c.QueryParam("code")
		state := c.QueryParam("state")
		if len(code) == 0 || len(state) == 0 {
			return herror.BadRequest("missing code or state")
		}

		cookie, err := c.Cookie(cookieName)
		if err != nil {
			return herror.BadRequest("missing cookie")
		}
		if cookie.Value != state {
			return herror.BadRequest("invalid state")
		}

		t, err := oac.Exchange(context.Background(), code)
		if err != nil {
			return herror.BadRequest("token exchange failed")
		}

		tu, err := p.FetchUserInfo(t)
		if err != nil {
			return herror.InternalServerError(err)
		}

		if !tu.IsLoginAllowedUser() {
			return c.String(http.StatusForbidden, "You are not permitted to access traQ")
		}

		sess, err := sessStore.GetSession(c, false)
		if err != nil {
			return herror.InternalServerError(err)
		}
		if sess != nil {
			if v, err := sess.Get(accountLinkingFlag); err != nil {
				return herror.InternalServerError(err)
			} else if v == true {
				// アカウント関連付けモード
				if err := sess.Delete(accountLinkingFlag); err != nil {
					return herror.InternalServerError(err)
				}
				if sess.UserID() == uuid.Nil {
					return herror.Unauthorized("You are not logged in. Please login.")
				}

				// ユーザーアカウント状態を確認
				user, err := repo.GetUser(sess.UserID(), false)
				if err != nil {
					return herror.InternalServerError(err)
				}
				if !user.IsActive() {
					return herror.Forbidden("this account is currently suspended")
				}

				// アカウントにリンク
				if err := repo.LinkExternalUserAccount(user.GetID(), repository.LinkExternalUserAccountArgs{
					ProviderName: tu.GetProviderName(),
					ExternalID:   tu.GetID(),
					Extra:        model.JSON{"externalName": tu.GetRawName()},
				}); err != nil {
					switch err {
					case repository.ErrAlreadyExists:
						return herror.BadRequest("this account has already been linked")
					default:
						return herror.InternalServerError(err)
					}
				}
				p.L().Info("an external user account has been linked to traQ user",
					zap.Stringer("id", user.GetID()),
					zap.String("name", user.GetName()),
					zap.String("providerName", tu.GetProviderName()),
					zap.String("externalId", tu.GetID()),
					zap.String("externalName", tu.GetRawName()))

				return c.Redirect(http.StatusFound, "/") // TODO リダイレクト先を設定画面に
			}
		}

		// ログインモード

		// ログインしていないことを確認
		if sess != nil && sess.UserID() != uuid.Nil {
			return herror.BadRequest("You have already logged in. Please logout once.")
		}

		user, err := repo.GetUserByExternalID(tu.GetProviderName(), tu.GetID(), false)
		if err != nil {
			if err != repository.ErrNotFound {
				return herror.InternalServerError(err)
			}

			if !allowSignUp {
				return herror.Unauthorized("You are not a member of traQ")
			}

			args := repository.CreateUserArgs{
				Name:        tu.GetName(),
				DisplayName: tu.GetDisplayName(),
				Role:        role.User,
				ExternalLogin: &model.ExternalProviderUser{
					ProviderName: tu.GetProviderName(),
					ExternalID:   tu.GetID(),
					Extra:        model.JSON{"externalName": tu.GetRawName()},
				},
			}
			if err := vd.Validate(args.Name, validator.UserNameRuleRequired...); err != nil {
				return herror.BadRequest("Your name doesn't match with traQ ID format")
			}

			if b, err := tu.GetProfileImage(); err == nil && b != nil {
				fid, err := processProfileIcon(fm, b)
				if err == nil {
					args.IconFileID = fid
				}
			}
			if args.IconFileID == uuid.Nil {
				fid, err := file.GenerateIconFile(fm, tu.GetName())
				if err != nil {
					return herror.InternalServerError(err)
				}
				args.IconFileID = fid
			}

			user, err = repo.CreateUser(args)
			if err != nil {
				if err == repository.ErrAlreadyExists {
					return herror.Conflict("name conflicts") // TODO 名前被りをどうするか
				}
				return herror.InternalServerError(err)
			}
			p.L().Info("New user was created by external auth",
				zap.Stringer("id", user.GetID()),
				zap.String("name", user.GetName()),
				zap.String("providerName", tu.GetProviderName()),
				zap.String("externalId", tu.GetID()),
				zap.String("externalName", tu.GetRawName()))
		}

		// ユーザーのアカウント状態の確認
		if !user.IsActive() {
			return herror.Forbidden("this account is currently suspended")
		}

		if _, err := sessStore.RenewSession(c, user.GetID()); err != nil {
			return herror.InternalServerError(err)
		}
		p.L().Info("User was logged in by external auth",
			zap.Stringer("id", user.GetID()),
			zap.String("name", user.GetName()),
			zap.String("providerName", tu.GetProviderName()),
			zap.String("externalId", tu.GetID()),
			zap.String("externalName", tu.GetRawName()))

		return c.Redirect(http.StatusFound, "/")
	}
}

func processProfileIcon(m file.Manager, src []byte) (uuid.UUID, error) {
	const maxImageSize = 256

	// デコード
	img, err := imaging.Decode(bytes.NewBuffer(src), imaging.AutoOrientation(true))
	if err != nil {
		return uuid.Nil, err
	}

	// リサイズ
	if size := img.Bounds().Size(); size.X > maxImageSize || size.Y > maxImageSize {
		img = imaging.Fit(img, maxImageSize, maxImageSize, imaging.Linear)
	}

	// PNGに戻す
	b := &bytes.Buffer{}
	_ = png.Encode(b, img)

	// ファイル保存
	f, err := m.Save(file.SaveArgs{
		FileName:  "icon",
		FileSize:  int64(b.Len()),
		MimeType:  consts.MimeImagePNG,
		FileType:  model.FileTypeIcon,
		Src:       bytes.NewReader(b.Bytes()),
		Thumbnail: img,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return f.GetID(), nil
}

func isTrue(s string) (b bool) {
	b, _ = strconv.ParseBool(s)
	return
}
