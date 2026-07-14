package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"
)

type ProfilePageData struct {
	Title   string
	Error   string
	Success string

	ProxyAuthenticated bool

	DisplayName string
	Nickname    string
	PhotoURL    template.URL

	LastName      string
	FirstName     string
	LastNameKana  string
	FirstNameKana string

	Email string
	Phone string

	PostalCode string
	Prefecture string
	City       string
	Street     string

	FieldErrors map[string]string
	FieldOK     map[string]bool
}

type ProfileInput struct {
	Nickname string

	LastNameKana  string
	FirstNameKana string

	Phone      string
	PostalCode string
	Prefecture string
	City       string
	Street     string

	PhotoChanged bool
	PhotoBytes   []byte

	CurrentPassword    string
	NewPassword        string
	NewPasswordConfirm string
}

type SaveProfileResult struct {
	FieldErrors      map[string]string
	FieldOK          map[string]bool
	FieldUnchanged   map[string]bool
	ValidationErrors map[string]bool
	ServerErrors     map[string]bool
}

var profileAttributes = []string{
	"displayName",
	"initials",
	"sn",
	"givenName",
	"mail",
	"telephoneNumber",
	"postalCode",
	"st",
	"l",
	"streetAddress",
	"thumbnailPhoto",
	"msDS-PhoneticLastName",
	"msDS-PhoneticFirstName",
}

func (app *App) loadProfile(userDN string) (ProfilePageData, error) {
	e, err := app.ldap.readEntry(userDN, profileAttributes)
	if err != nil {
		return ProfilePageData{}, err
	}

	photo := e.GetRawAttributeValue("thumbnailPhoto")
	var photoURL template.URL
	if len(photo) > 0 {
		photoURL = template.URL(
			"data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(photo),
		)
	}

	data := ProfilePageData{
		Title:       "プロフィール編集",
		FieldErrors: map[string]string{},
		FieldOK:     map[string]bool{},
		Nickname:    e.GetAttributeValue("initials"),
		DisplayName: e.GetAttributeValue("displayName"),
		LastName:    e.GetAttributeValue("sn"),
		FirstName:   e.GetAttributeValue("givenName"),
		PhotoURL:    photoURL,

		LastNameKana:  e.GetAttributeValue("msDS-PhoneticLastName"),
		FirstNameKana: e.GetAttributeValue("msDS-PhoneticFirstName"),
		Email:         e.GetAttributeValue("mail"),
		Phone:         e.GetAttributeValue("telephoneNumber"),
		PostalCode:    e.GetAttributeValue("postalCode"),
		Prefecture:    e.GetAttributeValue("st"),
		City:          e.GetAttributeValue("l"),
		Street:        e.GetAttributeValue("streetAddress"),
	}

	return data, nil
}
func (app *App) updateProfileStringField(
	result *SaveProfileResult,
	userDN string,
	fieldName string,
	ldapAttr string,
	oldValue string,
	newValue string,
) {
	modified, err := app.ldap.modifyStringAttribute(userDN, ldapAttr, oldValue, newValue)
	if err != nil {
		app.logger.Error("modify profile field failed",
			"user_dn", userDN,
			"field", fieldName,
			"ldap_attr", ldapAttr,
			"old_empty", strings.TrimSpace(oldValue) == "",
			"new_empty", strings.TrimSpace(newValue) == "",
			"error", err,
		)

		result.FieldErrors[fieldName] = "保存できませんでした。"
		result.ServerErrors[fieldName] = true
		return
	}

	if !modified {
		result.FieldUnchanged[fieldName] = true
		app.logger.Debug("profile field unchanged",
			"user_dn", userDN,
			"field", fieldName,
			"ldap_attr", ldapAttr,
		)
		return
	}

	app.logger.Info("modify profile field succeeded",
		"user_dn", userDN,
		"field", fieldName,
		"ldap_attr", ldapAttr,
		"old_empty", strings.TrimSpace(oldValue) == "",
		"new_empty", strings.TrimSpace(newValue) == "",
	)

	result.FieldOK[fieldName] = true
}

func (app *App) updateProfileBinaryField(
	result *SaveProfileResult,
	userDN string,
	fieldName string,
	ldapAttr string,
	newValue []byte,
) {
	modified, err := app.ldap.modifyBinaryAttribute(userDN, ldapAttr, newValue)
	if err != nil {
		app.logger.Error("modify profile binary field failed",
			"user_dn", userDN,
			"field", fieldName,
			"ldap_attr", ldapAttr,
			"bytes", len(newValue),
			"error", err,
		)

		result.FieldErrors[fieldName] = "画像を保存できませんでした。"
		result.ServerErrors[fieldName] = true
		return
	}

	if !modified {
		result.FieldUnchanged[fieldName] = true
		app.logger.Debug("profile binary field unchanged",
			"user_dn", userDN,
			"field", fieldName,
			"ldap_attr", ldapAttr,
		)
		return
	}

	app.logger.Info("modify profile binary field succeeded",
		"user_dn", userDN,
		"field", fieldName,
		"ldap_attr", ldapAttr,
		"bytes", len(newValue),
	)

	result.FieldOK[fieldName] = true
}
func (app *App) showProfile(w http.ResponseWriter, r *http.Request) {
	userDN := app.sessionUser(r.Context()).UserDN
	if userDN == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	data, err := app.loadProfile(userDN)
	if err != nil {
		if isLDAPNoSuchObject(err) {
			app.redirectAfterInvalidUserDN(w, r)
			return
		}

		app.logger.Error("load profile failed",
			"user_dn", userDN,
			"error", err,
		)

		app.render(w, http.StatusInternalServerError, "profile", ProfilePageData{
			Title:              "プロフィール編集",
			Error:              "プロフィール情報を読み込めませんでした。",
			ProxyAuthenticated: requestUsesProxyAuth(r.Context()),
			FieldErrors:        map[string]string{},
			FieldOK:            map[string]bool{},
		})
		return
	}

	if r.URL.Query().Get("session") == "refreshed" {
		data.Success = "AD のユーザー情報が変更されたため、ログイン情報を更新しました。"
	} else if r.URL.Query().Get("password") == "changed" {
		data.Success = "パスワードを変更しました。"
	}
	data.ProxyAuthenticated = requestUsesProxyAuth(r.Context())
	app.render(w, http.StatusOK, "profile", data)
}

func (app *App) redirectAfterInvalidUserDN(w http.ResponseWriter, r *http.Request) {
	user := app.sessionUser(r.Context())
	if err := app.destroySession(r.Context()); err != nil {
		app.logger.Error("destroy invalid user session failed",
			"username", user.Username,
			"user_dn", user.UserDN,
			"error", err,
		)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	app.logger.Warn("session user DN is no longer valid",
		"username", user.Username,
		"user_dn", user.UserDN,
	)

	if requestUsesProxyAuth(r.Context()) {
		http.Redirect(w, r, "/profile?session=refreshed", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/login?session=invalid", http.StatusSeeOther)
}

func restoreFailedInputFields(data *ProfilePageData, input ProfileInput, result SaveProfileResult) {
	if result.FieldErrors["nickname"] != "" {
		data.Nickname = input.Nickname
	}
	if result.FieldErrors["last_name_kana"] != "" {
		data.LastNameKana = input.LastNameKana
	}
	if result.FieldErrors["first_name_kana"] != "" {
		data.FirstNameKana = input.FirstNameKana
	}
	if result.FieldErrors["phone"] != "" {
		data.Phone = input.Phone
	}
	if result.FieldErrors["postal_code"] != "" {
		data.PostalCode = input.PostalCode
	}
	if result.FieldErrors["prefecture"] != "" {
		data.Prefecture = input.Prefecture
	}
	if result.FieldErrors["city"] != "" {
		data.City = input.City
	}
	if result.FieldErrors["street"] != "" {
		data.Street = input.Street
	}
}

func profilePageDataFromInput(input ProfileInput) ProfilePageData {
	return ProfilePageData{
		Nickname:      input.Nickname,
		LastNameKana:  input.LastNameKana,
		FirstNameKana: input.FirstNameKana,

		Phone:      input.Phone,
		PostalCode: input.PostalCode,
		Prefecture: input.Prefecture,
		City:       input.City,
		Street:     input.Street,
	}
}

const maxThumbnailBytes = 96 << 10 // 96 KiB
func decodePhotoDataURL(s string) ([]byte, error) {
	const prefix = "data:image/jpeg;base64,"

	if !strings.HasPrefix(s, prefix) {
		return nil, errors.New("invalid photo data URL")
	}

	raw := strings.TrimPrefix(s, prefix)

	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode photo base64: %w", err)
	}

	if len(b) == 0 {
		return nil, errors.New("empty photo")
	}

	if len(b) > maxThumbnailBytes {
		return nil, fmt.Errorf("photo too large: %d bytes", len(b))
	}

	return b, nil
}

func (app *App) postProfile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		app.logger.Warn("parse profile form failed",
			"content_length", r.ContentLength,
			"remote", r.RemoteAddr,
			"error", err,
		)
		app.render(w, http.StatusBadRequest, "profile", ProfilePageData{
			Title:              "プロフィール編集",
			Error:              "フォームの読み取りに失敗しました。",
			ProxyAuthenticated: requestUsesProxyAuth(r.Context()),
			FieldErrors:        map[string]string{},
			FieldOK:            map[string]bool{},
		})
		return
	}

	userDN := app.sessionUser(r.Context()).UserDN
	if userDN == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	photoChanged := r.FormValue("photo_changed") == "1"

	input := ProfileInput{
		Nickname: r.FormValue("nickname"),

		LastNameKana:  r.FormValue("last_name_kana"),
		FirstNameKana: r.FormValue("first_name_kana"),

		Phone:      r.FormValue("phone"),
		PostalCode: r.FormValue("postal_code"),
		Prefecture: r.FormValue("prefecture"),
		City:       r.FormValue("city"),
		Street:     r.FormValue("street"),

		PhotoChanged: photoChanged,

		CurrentPassword:    r.FormValue("current_password"),
		NewPassword:        r.FormValue("new_password"),
		NewPasswordConfirm: r.FormValue("new_password_confirm"),
	}

	var photoInputError string
	if photoChanged {
		photoBytes, err := decodePhotoDataURL(r.FormValue("photo_base64"))
		if err != nil {
			app.logger.Info("invalid profile photo",
				"user_dn", userDN,
				"error", err,
			)
			input.PhotoChanged = false
			photoInputError = "画像サイズを確認できませんでした。画像を選び直してください。"
		} else {
			input.PhotoBytes = photoBytes
		}
	}

	current, err := app.loadProfile(userDN)
	if err != nil {
		if isLDAPNoSuchObject(err) {
			app.redirectAfterInvalidUserDN(w, r)
			return
		}

		app.logger.Error("load current profile before save failed",
			"user_dn", userDN,
			"error", err,
		)

		data := profilePageDataFromInput(input)
		data.Title = "プロフィール編集"
		data.Error = "現在のプロフィール情報を読み込めなかったため、保存できませんでした。"
		data.ProxyAuthenticated = requestUsesProxyAuth(r.Context())
		data.FieldErrors = map[string]string{}
		data.FieldOK = map[string]bool{}

		app.render(w, http.StatusInternalServerError, "profile", data)
		return
	}

	result := app.saveProfileFields(userDN, current, input)
	if photoInputError != "" {
		result.FieldErrors["photo"] = photoInputError
		result.ValidationErrors["photo"] = true
	}

	data, err := app.loadProfile(userDN)
	if isLDAPNoSuchObject(err) {
		app.redirectAfterInvalidUserDN(w, r)
		return
	}
	reloadFailed := err != nil
	if err != nil {
		app.logger.Error("reload profile after save failed",
			"user_dn", userDN,
			"error", err,
		)

		data = current
		data.Error = "保存後のプロフィール情報を読み込めませんでした。"
	}

	data.FieldErrors = result.FieldErrors
	data.FieldOK = result.FieldOK

	// 保存失敗した項目は、AD再読込値ではなく入力値を画面に戻す。
	restoreFailedInputFields(&data, input, result)

	changedCount := len(result.FieldOK) + len(result.FieldErrors)

	switch {
	case len(result.FieldErrors) == 0 && len(result.FieldOK) > 0:
		data.Success = "保存しました。"
	case changedCount == 0:
		data.Success = "変更はありませんでした。"
	case len(result.FieldErrors) > 0 && len(result.FieldOK) > 0:
		data.Error = "一部の項目を保存できませんでした。"
	default:
		data.Error = "保存できませんでした。"
	}

	status := profileSaveStatus(result, reloadFailed)
	app.logProfileSaveResult(r, userDN, status, result, reloadFailed)
	if status == http.StatusOK && result.FieldOK["password"] {
		proxyAuthenticated := requestUsesProxyAuth(r.Context())
		if err := app.destroySession(r.Context()); err != nil {
			app.logger.Error("destroy session after password change failed",
				"user_dn", userDN,
				"error", err,
			)
			data.Error = "パスワードは変更されましたが、ログアウト処理に失敗しました。手動でログアウトしてください。"
			app.render(w, http.StatusInternalServerError, "profile", data)
			return
		}

		if proxyAuthenticated {
			// プロキシー認証はADパスワードを使わないため、新しい匿名状態から
			// プロキシーに再認証させ、プロフィール画面で成功を通知する。
			http.Redirect(w, r, "/profile?password=changed", http.StatusSeeOther)
			return
		}

		app.sessionManager.Put(r.Context(), "login_success",
			"パスワードを変更しました。新しいパスワードでログインしてください。")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	data.ProxyAuthenticated = requestUsesProxyAuth(r.Context())
	app.render(w, status, "profile", data)
}

func profileSaveStatus(result SaveProfileResult, reloadFailed bool) int {
	if reloadFailed || len(result.ServerErrors) > 0 {
		return http.StatusInternalServerError
	}
	if len(result.ValidationErrors) > 0 {
		return http.StatusBadRequest
	}

	// 未分類の保存エラーを誤って成功扱いしない。
	if len(result.FieldErrors) > 0 {
		return http.StatusInternalServerError
	}

	return http.StatusOK
}

func sortedFieldNames[T any](fields map[string]T) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (app *App) logProfileSaveResult(
	r *http.Request,
	userDN string,
	status int,
	result SaveProfileResult,
	reloadFailed bool,
) {
	level := slog.LevelInfo
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	} else if status >= http.StatusBadRequest {
		level = slog.LevelWarn
	}

	app.logger.Log(r.Context(), level, "profile save completed",
		"user_dn", userDN,
		"status", status,
		"saved_count", len(result.FieldOK),
		"saved_fields", sortedFieldNames(result.FieldOK),
		"unchanged_count", len(result.FieldUnchanged),
		"unchanged_fields", sortedFieldNames(result.FieldUnchanged),
		"error_count", len(result.FieldErrors),
		"error_fields", sortedFieldNames(result.FieldErrors),
		"validation_error_fields", sortedFieldNames(result.ValidationErrors),
		"server_error_fields", sortedFieldNames(result.ServerErrors),
		"reload_failed", reloadFailed,
	)
}

func (app *App) saveProfileFields(userDN string, current ProfilePageData, input ProfileInput) SaveProfileResult {
	result := SaveProfileResult{
		FieldErrors:      map[string]string{},
		FieldOK:          map[string]bool{},
		FieldUnchanged:   map[string]bool{},
		ValidationErrors: map[string]bool{},
		ServerErrors:     map[string]bool{},
	}

	app.changeOwnPassword(&result, userDN, input)

	app.updateProfileStringField(&result, userDN, "nickname", "initials", current.Nickname, input.Nickname)

	app.updateProfileStringField(&result, userDN, "last_name_kana", "msDS-PhoneticLastName", current.LastNameKana, input.LastNameKana)
	app.updateProfileStringField(&result, userDN, "first_name_kana", "msDS-PhoneticFirstName", current.FirstNameKana, input.FirstNameKana)

	app.updateProfileStringField(&result, userDN, "phone", "telephoneNumber", current.Phone, input.Phone)
	app.updateProfileStringField(&result, userDN, "postal_code", "postalCode", current.PostalCode, input.PostalCode)
	app.updateProfileStringField(&result, userDN, "prefecture", "st", current.Prefecture, input.Prefecture)
	app.updateProfileStringField(&result, userDN, "city", "l", current.City, input.City)
	app.updateProfileStringField(&result, userDN, "street", "streetAddress", current.Street, input.Street)

	if input.PhotoChanged {
		app.updateProfileBinaryField(&result, userDN, "photo", "thumbnailPhoto", input.PhotoBytes)
	}

	return result
}

func passwordChangeRequested(input ProfileInput) bool {
	return input.CurrentPassword != "" || input.NewPassword != "" || input.NewPasswordConfirm != ""
}

func validatePasswordChange(input ProfileInput) map[string]string {
	fieldErrors := map[string]string{}
	if !passwordChangeRequested(input) {
		return fieldErrors
	}

	if input.CurrentPassword == "" {
		fieldErrors["current_password"] = "現在のパスワードを入力してください。"
	}
	if input.NewPassword == "" {
		fieldErrors["new_password"] = "新しいパスワードを入力してください。"
	}
	if input.NewPasswordConfirm == "" {
		fieldErrors["new_password_confirm"] = "確認用パスワードを入力してください。"
	} else if input.NewPassword != input.NewPasswordConfirm {
		fieldErrors["new_password_confirm"] = "新しいパスワードが一致しません。"
	}
	if input.CurrentPassword != "" && input.CurrentPassword == input.NewPassword {
		fieldErrors["new_password"] = "現在とは異なるパスワードを入力してください。"
	}

	return fieldErrors
}

func passwordTooYoungMessage(allowedAt time.Time) string {
	return fmt.Sprintf(
		"ADの最小変更間隔により、パスワードは%s以降に変更できます。",
		allowedAt.Format("2006年1月2日 15:04:05 MST"),
	)
}

func (app *App) changeOwnPassword(result *SaveProfileResult, userDN string, input ProfileInput) {
	if !passwordChangeRequested(input) {
		return
	}

	validationErrors := validatePasswordChange(input)
	for field, message := range validationErrors {
		result.FieldErrors[field] = message
		result.ValidationErrors[field] = true
	}
	if len(validationErrors) > 0 {
		return
	}

	err := app.ldap.changeOwnPassword(userDN, input.CurrentPassword, input.NewPassword)
	switch {
	case err == nil:
		app.logger.Info("password change succeeded", "user_dn", userDN)
		result.FieldOK["password"] = true
	case errors.Is(err, errCurrentPasswordInvalid):
		app.logger.Warn("password change rejected",
			"user_dn", userDN,
			"reason", "invalid_current_password",
			"error", err,
		)
		result.FieldErrors["current_password"] = "現在のパスワードが正しくありません。"
		result.ValidationErrors["current_password"] = true
	case errors.Is(err, errPasswordTooYoung):
		app.logger.Warn("password change rejected",
			"user_dn", userDN,
			"reason", "password_too_young",
			"error", err,
		)

		message := "前回のパスワード変更から時間が経っていないため、まだ変更できません。時間をおいて再試行してください。"
		allowedAt, allowedAtErr := app.ldap.passwordChangeAllowedAt(userDN)
		if allowedAtErr != nil {
			app.logger.Warn("password change availability unavailable",
				"user_dn", userDN,
				"error", allowedAtErr,
			)
		} else if allowedAt.After(time.Now()) {
			localAllowedAt := allowedAt.In(time.Local)
			message = passwordTooYoungMessage(localAllowedAt)
			app.logger.Info("password change availability calculated",
				"user_dn", userDN,
				"allowed_at", localAllowedAt.Format(time.RFC3339),
			)
		} else {
			app.logger.Warn("password change availability is not in the future",
				"user_dn", userDN,
				"allowed_at", allowedAt.Format(time.RFC3339),
			)
		}

		result.FieldErrors["new_password"] = message
		result.ValidationErrors["new_password"] = true
	case errors.Is(err, errPasswordPolicyViolation):
		app.logger.Warn("password change rejected",
			"user_dn", userDN,
			"reason", "password_policy",
			"error", err,
		)
		result.FieldErrors["new_password"] = "パスワードの長さ、複雑性、履歴などの条件を満たしていません。"
		result.ValidationErrors["new_password"] = true
	default:
		app.logger.Error("password change failed",
			"user_dn", userDN,
			"error", err,
		)
		result.FieldErrors["new_password"] = "パスワードを変更できませんでした。"
		result.ServerErrors["new_password"] = true
	}
}
