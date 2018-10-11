package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/satori/go.uuid"
	"github.com/traPtitech/traQ/model"
)

func TestPutNotificationStatus(t *testing.T) {
	e, cookie, mw, assert, require := beforeTest(t)

	channel := mustMakeChannelDetail(t, testUser.GetUID(), "subscribing", "")
	userID := mustCreateUser(t, "poyo").ID

	post := struct {
		On  []string
		Off []string
	}{On: []string{userID}, Off: nil}

	body, err := json.Marshal(post)
	require.NoError(err)

	req := httptest.NewRequest("PUT", "http://test", bytes.NewReader(body))
	c, rec := getContext(e, t, cookie, req)
	c.SetPath("/channels/:ID/notification")
	c.SetParamNames("ID")
	c.SetParamValues(channel.ID.String())
	requestWithContext(t, mw(PutNotificationStatus), c)

	if assert.EqualValues(http.StatusNoContent, rec.Code, rec.Body.String()) {
		users, err := model.GetSubscribingUser(channel.ID)
		require.NoError(err)
		assert.EqualValues(users, []uuid.UUID{uuid.FromStringOrNil(userID)})
	}

}

func TestGetNotificationStatus(t *testing.T) {
	e, cookie, mw, assert, require := beforeTest(t)

	channel := mustMakeChannelDetail(t, testUser.GetUID(), "subscribing", "")
	user := mustCreateUser(t, "poyo")

	require.NoError(model.SubscribeChannel(user.GetUID(), channel.ID))
	require.NoError(model.SubscribeChannel(testUser.GetUID(), channel.ID))

	c, rec := getContext(e, t, cookie, nil)
	c.Set("channel", channel)

	requestWithContext(t, mw(GetNotificationStatus), c)

	if assert.EqualValues(http.StatusOK, rec.Code, rec.Body.String()) {
		var res []string
		require.NoError(json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Len(res, 2)
	}
}

func TestGetNotificationChannels(t *testing.T) {
	e, cookie, mw, assert, require := beforeTest(t)

	require.NoError(model.SubscribeChannel(testUser.GetUID(), mustMakeChannelDetail(t, testUser.GetUID(), "subscribing", "").ID))
	require.NoError(model.SubscribeChannel(testUser.GetUID(), mustMakeChannelDetail(t, testUser.GetUID(), "subscribing2", "").ID))

	c, rec := getContext(e, t, cookie, nil)
	c.Set("targetUserID", testUser.ID)

	requestWithContext(t, mw(GetNotificationChannels), c)

	if assert.EqualValues(http.StatusOK, rec.Code, rec.Body.String()) {
		var res []ChannelForResponse
		require.NoError(json.Unmarshal(rec.Body.Bytes(), &res))
		assert.Len(res, 2)
	}
}
