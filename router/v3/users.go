package v3

import (
	"context"
	"github.com/dgrijalva/jwt-go"
	vd "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"
	"github.com/skip2/go-qrcode"
	"github.com/traPtitech/traQ/model"
	"github.com/traPtitech/traQ/repository"
	"github.com/traPtitech/traQ/router/consts"
	"github.com/traPtitech/traQ/router/extension/herror"
	"github.com/traPtitech/traQ/router/utils"
	jwt2 "github.com/traPtitech/traQ/utils/jwt"
	"github.com/traPtitech/traQ/utils/optional"
	"github.com/traPtitech/traQ/utils/validator"
	"net/http"
	"time"
)

// GetUsers GET /users
func (h *Handlers) GetUsers(c echo.Context) error {
	q := repository.UsersQuery{}

	if !isTrue(c.QueryParam("include-suspended")) {
		q = q.Active()
	}

	users, err := h.Repo.GetUsers(q)
	if err != nil {
		return herror.InternalServerError(err)
	}
	return c.JSON(http.StatusOK, formatUsers(users))
}

// GetMe GET /users/me
func (h *Handlers) GetMe(c echo.Context) error {
	me := getRequestUser(c)

	tags, err := h.Repo.GetUserTagsByUserID(me.GetID())
	if err != nil {
		return herror.InternalServerError(err)
	}

	groups, err := h.Repo.GetUserBelongingGroupIDs(me.GetID())
	if err != nil {
		return herror.InternalServerError(err)
	}

	return c.JSON(http.StatusOK, echo.Map{
		"id":          me.GetID(),
		"bio":         me.GetBio(),
		"groups":      groups,
		"tags":        formatUserTags(tags),
		"updatedAt":   me.GetUpdatedAt(),
		"lastOnline":  me.GetLastOnline(),
		"twitterId":   me.GetTwitterID(),
		"name":        me.GetName(),
		"displayName": me.GetResponseDisplayName(),
		"iconFileId":  me.GetIconFileID(),
		"bot":         me.IsBot(),
		"state":       me.GetState().Int(),
		"permissions": h.RBAC.GetGrantedPermissions(me.GetRole()),
		"homeChannel": me.GetHomeChannel(),
	})
}

// PatchMeRequest PATCH /users/me リクエストボディ
type PatchMeRequest struct {
	DisplayName optional.String `json:"displayName"`
	TwitterID   optional.String `json:"twitterId"`
	Bio         optional.String `json:"bio"`
	HomeChannel optional.UUID   `json:"homeChannel"`
}

func (r PatchMeRequest) ValidateWithContext(ctx context.Context) error {
	return vd.ValidateStructWithContext(ctx, &r,
		vd.Field(&r.DisplayName, vd.RuneLength(0, 64)),
		vd.Field(&r.TwitterID, validator.TwitterIDRule...),
		vd.Field(&r.Bio, vd.RuneLength(0, 1000)),
	)
}

// EditMe PATCH /users/me
func (h *Handlers) EditMe(c echo.Context) error {
	userID := getRequestUserID(c)

	var req PatchMeRequest
	if err := bindAndValidate(c, &req); err != nil {
		return err
	}

	if req.HomeChannel.Valid {
		if req.HomeChannel.UUID != uuid.Nil {
			// チャンネル存在確認
			if !h.Repo.GetPublicChannelTree().IsChannelPresent(req.HomeChannel.UUID) {
				return herror.BadRequest("invalid homeChannel")
			}
		}
	}

	args := repository.UpdateUserArgs{
		DisplayName: req.DisplayName,
		TwitterID:   req.TwitterID,
		Bio:         req.Bio,
		HomeChannel: req.HomeChannel,
	}
	if err := h.Repo.UpdateUser(userID, args); err != nil {
		return herror.InternalServerError(err)
	}

	return c.NoContent(http.StatusNoContent)
}

// PutMyPasswordRequest PUT /users/me/password リクエストボディ
type PutMyPasswordRequest struct {
	Password    string `json:"password"`
	NewPassword string `json:"newPassword"`
}

func (r PutMyPasswordRequest) Validate() error {
	return vd.ValidateStruct(&r,
		vd.Field(&r.Password, vd.Required),
		vd.Field(&r.NewPassword, validator.PasswordRuleRequired...),
	)
}

// ChangeMyPassword PUT /users/me/password
func (h *Handlers) PutMyPassword(c echo.Context) error {
	var req PutMyPasswordRequest
	if err := bindAndValidate(c, &req); err != nil {
		return err
	}

	user := getRequestUser(c)

	// パスワード認証
	if err := user.Authenticate(req.Password); err != nil {
		return herror.Unauthorized("password is wrong")
	}

	return utils.ChangeUserPassword(c, h.Repo, user.GetID(), req.NewPassword)
}

// GetMyQRCode GET /users/me/qr-code
func (h *Handlers) GetMyQRCode(c echo.Context) error {
	user := getRequestUser(c)

	// トークン生成
	now := time.Now()
	deadline := now.Add(5 * time.Minute)
	token, err := jwt2.Sign(jwt.MapClaims{
		"iat":         now.Unix(),
		"exp":         deadline.Unix(),
		"userId":      user.GetID(),
		"name":        user.GetName(),
		"displayName": user.GetDisplayName(),
	})
	if err != nil {
		return herror.InternalServerError(err)
	}

	if isTrue(c.QueryParam("token")) {
		// 画像じゃなくて生のトークンを返す
		return c.String(http.StatusOK, token)
	}

	// QRコード画像生成
	png, err := qrcode.Encode(token, qrcode.Low, 512)
	if err != nil {
		return herror.InternalServerError(err)
	}
	return c.Blob(http.StatusOK, consts.MimeImagePNG, png)
}

// GetUserIcon GET /users/:userID/icon
func (h *Handlers) GetUserIcon(c echo.Context) error {
	return utils.ServeUserIcon(c, h.Repo, getParamUser(c))
}

// ChangeUserIcon PUT /users/:userID/icon
func (h *Handlers) ChangeUserIcon(c echo.Context) error {
	return utils.ChangeUserIcon(h.Imaging, c, h.Repo, getParamAsUUID(c, consts.ParamUserID))
}

// GetMyIcon GET /users/me/icon
func (h *Handlers) GetMyIcon(c echo.Context) error {
	return utils.ServeUserIcon(c, h.Repo, getRequestUser(c))
}

// ChangeMyIcon PUT /users/me/icon
func (h *Handlers) ChangeMyIcon(c echo.Context) error {
	return utils.ChangeUserIcon(h.Imaging, c, h.Repo, getRequestUserID(c))
}

// GetMyStampHistory GET /users/me/stamp-history リクエストクエリ
type GetMyStampHistoryRequest struct {
	Limit int `query:"limit"`
}

func (r *GetMyStampHistoryRequest) Validate() error {
	if r.Limit == 0 {
		r.Limit = 100
	}
	return vd.ValidateStruct(r,
		vd.Field(&r.Limit, vd.Min(1), vd.Max(100)),
	)
}

// GetMyStampHistory GET /users/me/stamp-history
func (h *Handlers) GetMyStampHistory(c echo.Context) error {
	var req GetMyStampHistoryRequest
	if err := bindAndValidate(c, &req); err != nil {
		return err
	}

	userID := getRequestUserID(c)
	history, err := h.Repo.GetUserStampHistory(userID, req.Limit)
	if err != nil {
		return herror.InternalServerError(err)
	}

	return c.JSON(http.StatusOK, history)
}

// PostMyFCMDeviceRequest POST /users/me/fcm-device リクエストボディ
type PostMyFCMDeviceRequest struct {
	Token string `json:"token"`
}

func (r PostMyFCMDeviceRequest) Validate() error {
	return vd.ValidateStruct(&r,
		vd.Field(&r.Token, vd.Required, vd.RuneLength(1, 190)),
	)
}

// PostMyFCMDevice POST /users/me/fcm-device
func (h *Handlers) PostMyFCMDevice(c echo.Context) error {
	var req PostMyFCMDeviceRequest
	if err := bindAndValidate(c, &req); err != nil {
		return err
	}

	userID := getRequestUserID(c)
	if err := h.Repo.RegisterDevice(userID, req.Token); err != nil {
		switch {
		case repository.IsArgError(err):
			return herror.BadRequest(err)
		default:
			return herror.InternalServerError(err)
		}
	}

	return c.NoContent(http.StatusNoContent)
}

// PutUserPasswordRequest PUT /users/:userID/password リクエストボディ
type PutUserPasswordRequest struct {
	NewPassword string `json:"newPassword"`
}

func (r PutUserPasswordRequest) Validate() error {
	return vd.ValidateStruct(&r,
		vd.Field(&r.NewPassword, validator.PasswordRuleRequired...),
	)
}

// ChangeUserPassword PUT /users/:userID/password
func (h *Handlers) ChangeUserPassword(c echo.Context) error {
	var req PutUserPasswordRequest
	if err := bindAndValidate(c, &req); err != nil {
		return err
	}
	return utils.ChangeUserPassword(c, h.Repo, getParamAsUUID(c, consts.ParamUserID), req.NewPassword)
}

// GetUser GET /users/:userID
func (h *Handlers) GetUser(c echo.Context) error {
	user := getParamUser(c)

	tags, err := h.Repo.GetUserTagsByUserID(user.GetID())
	if err != nil {
		return herror.InternalServerError(err)
	}

	groups, err := h.Repo.GetUserBelongingGroupIDs(user.GetID())
	if err != nil {
		return herror.InternalServerError(err)
	}

	return c.JSON(http.StatusOK, formatUserDetail(user, tags, groups))
}

// PatchUserRequest PATCH /users/:userID リクエストボディ
type PatchUserRequest struct {
	DisplayName optional.String `json:"displayName"`
	TwitterID   optional.String `json:"twitterId"`
	Role        optional.String `json:"role"`
	State       optional.Int    `json:"state"`
}

func (r PatchUserRequest) Validate() error {
	return vd.ValidateStruct(&r,
		vd.Field(&r.DisplayName, vd.RuneLength(0, 64)),
		vd.Field(&r.TwitterID, validator.TwitterIDRule...),
		vd.Field(&r.Role, vd.RuneLength(0, 30)),
		vd.Field(&r.State, vd.Min(0), vd.Max(2)),
	)
}

// EditUser PATCH /users/:userID
func (h *Handlers) EditUser(c echo.Context) error {
	userID := getParamAsUUID(c, consts.ParamUserID)

	var req PatchUserRequest
	if err := bindAndValidate(c, &req); err != nil {
		return err
	}

	args := repository.UpdateUserArgs{
		DisplayName: req.DisplayName,
		TwitterID:   req.TwitterID,
		Role:        req.Role,
	}
	if req.State.Valid {
		args.UserState.Valid = true
		args.UserState.State = model.UserAccountStatus(req.State.Int64)
	}

	if err := h.Repo.UpdateUser(userID, args); err != nil {
		return herror.InternalServerError(err)
	}

	return c.NoContent(http.StatusNoContent)
}

// GetMyChannelSubscriptions GET /users/me/subscriptions
func (h *Handlers) GetMyChannelSubscriptions(c echo.Context) error {
	subscriptions, err := h.Repo.GetChannelSubscriptions(repository.ChannelSubscriptionQuery{}.SetUser(getRequestUserID(c)))
	if err != nil {
		return herror.InternalServerError(err)
	}

	type response struct {
		ChannelID uuid.UUID `json:"channelId"`
		Level     int       `json:"level"`
	}
	result := make([]response, len(subscriptions))
	for i, subscription := range subscriptions {
		result[i] = response{ChannelID: subscription.ChannelID, Level: subscription.GetLevel().Int()}
	}

	return c.JSON(http.StatusOK, result)
}

// PutChannelSubscribeLevelRequest PUT /users/me/subscriptions/:channelID リクエストボディ
type PutChannelSubscribeLevelRequest struct {
	Level optional.Int `json:"level"`
}

func (r PutChannelSubscribeLevelRequest) Validate() error {
	return vd.ValidateStruct(&r,
		vd.Field(&r.Level, vd.NotNil, vd.Min(0), vd.Max(2)),
	)
}

// SetChannelSubscribeLevel PUT /users/me/subscriptions/:channelID
func (h *Handlers) SetChannelSubscribeLevel(c echo.Context) error {
	channelID := getParamAsUUID(c, consts.ParamChannelID)

	var req PutChannelSubscribeLevelRequest
	if err := bindAndValidate(c, &req); err != nil {
		return err
	}

	ch, err := h.Repo.GetChannel(channelID)
	if err != nil {
		if err == repository.ErrNotFound {
			return herror.NotFound()
		}
		return herror.InternalServerError(err)
	}

	if ch.IsForced || !ch.IsPublic {
		return herror.Forbidden()
	}

	args := repository.ChangeChannelSubscriptionArgs{
		UpdaterID:    getRequestUserID(c),
		Subscription: map[uuid.UUID]model.ChannelSubscribeLevel{getRequestUserID(c): model.ChannelSubscribeLevel(req.Level.Int64)},
	}
	if err := h.Repo.ChangeChannelSubscription(ch.ID, args); err != nil {
		return herror.InternalServerError(err)
	}
	return c.NoContent(http.StatusNoContent)
}
