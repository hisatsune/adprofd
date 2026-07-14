package main

import (
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/go-ldap/ldap/v3"
)

type ldapDAO struct {
	config LDAPConfig
	logger *slog.Logger
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,128}$`)

var (
	errCurrentPasswordInvalid  = errors.New("current password is invalid")
	errPasswordTooYoung        = errors.New("password is too young to change")
	errPasswordPolicyViolation = errors.New("password violates AD policy")
)

func validateLoginName(username string) error {
	if !usernamePattern.MatchString(username) {
		return errors.New("invalid username format")
	}
	return nil
}

func validateLDAPSURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse LDAP URL: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "ldaps") {
		return fmt.Errorf("LDAP URL must use ldaps scheme")
	}
	return nil
}

func (dao *ldapDAO) dialLDAP() (*ldap.Conn, error) {
	cfg := dao.config
	if err := validateLDAPSURL(cfg.URL); err != nil {
		return nil, err
	}

	conn, err := ldap.DialURL(cfg.URL, ldap.DialWithTLSConfig(&tls.Config{
		ServerName:         cfg.ServerName,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: false,
	}))
	if err != nil {
		dao.logger.Debug("LDAP CONNECTION Failed", "error", err)
		return nil, fmt.Errorf("ldap dial: %w", err)
	}

	conn.SetTimeout(5 * time.Second)
	return conn, nil
}

func (dao *ldapDAO) findUserDN(username string) (string, error) {
	dao.logger.Debug("FIND_USER_DN")
	if err := validateLoginName(username); err != nil {
		dao.logger.Debug("INVALID Login name", "error", err)
		return "", err
	}

	conn, err := dao.dialLDAP()
	if err != nil {
		dao.logger.Debug("LDAP DIAL Failed", "error", err)
		return "", err
	}
	defer conn.Close()

	cfg := dao.config
	dao.logger.Debug("START LDAP BIND WITH: " + cfg.BindDN)
	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		dao.logger.Debug("LDAP BIND Failed", "error", err)
		return "", fmt.Errorf("service bind: %w", err)
	}

	escaped := ldap.EscapeFilter(username)

	filter := fmt.Sprintf(
		"(&(objectClass=user)(!(objectClass=computer))(|(sAMAccountName=%s)(userPrincipalName=%s)))",
		escaped,
		escaped,
	)

	req := ldap.NewSearchRequest(
		cfg.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		2,
		5,
		false,
		filter,
		[]string{"distinguishedName", "sAMAccountName", "userPrincipalName", "displayName"},
		nil,
	)

	res, err := conn.Search(req)
	if err != nil {
		dao.logger.Debug("LDAP SEARCH Failed", "error", err)
		return "", fmt.Errorf("user search: %w", err)
	}

	if len(res.Entries) == 0 {
		dao.logger.Debug("LDAP SERCH Failed: USER NOT FOUND")
		return "", errors.New("user not found")
	}

	if len(res.Entries) > 1 {
		dao.logger.Debug("LDAP SEARCH Failed: USER MULTIPLE FOUND")
		return "", errors.New("multiple users matched")
	}

	return res.Entries[0].DN, nil
}

func (dao *ldapDAO) authenticateLDAP(username, password string) (string, error) {
	dao.logger.Debug("AUTHENTICATE_LDAP")
	username = strings.TrimSpace(username)

	if username == "" || password == "" {
		return "", errors.New("empty username or password")
	}

	userDN, err := dao.findUserDN(username)
	if err != nil {
		dao.logger.Debug("LDAP FIND USER Failed", "error", err)
		return "", err
	}

	conn, err := dao.dialLDAP()
	if err != nil {
		dao.logger.Debug("LDAP DIAL Failed", "error", err)
		return "", err
	}
	defer conn.Close()

	if err := conn.Bind(userDN, password); err != nil {
		dao.logger.Debug("LDAP BIND Failed", "error", err)
		return "", fmt.Errorf("user bind: %w", err)
	}

	return userDN, nil
}

func (dao *ldapDAO) replaceOrDeleteLDAPBinaryAttribute(userDN, attr string, value []byte) error {
	conn, err := dao.dialLDAP()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.Bind(dao.config.BindDN, dao.config.BindPassword); err != nil {
		return fmt.Errorf("service bind: %w", err)
	}

	req := ldap.NewModifyRequest(userDN, nil)
	if len(value) == 0 {
		req.Delete(attr, nil)
	} else {
		req.Replace(attr, []string{string(value)})
	}

	if err := conn.Modify(req); err != nil {
		return fmt.Errorf("modify %s failed: %w", attr, err)
	}

	return nil
}

func (dao *ldapDAO) replaceOrDeleteLDAPAttribute(userDN, attr, value string) error {
	conn, err := dao.dialLDAP()
	if err != nil {
		return err
	}
	defer conn.Close()

	cfg := dao.config
	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		return fmt.Errorf("service bind: %w", err)
	}

	value = strings.TrimSpace(value)
	dao.logger.Info("ldap modify attempt",
		"user_dn", userDN,
		"attr", attr,
		"value_empty", value == "",
	)

	req := ldap.NewModifyRequest(userDN, nil)
	if value == "" {
		req.Delete(attr, nil)
	} else {
		req.Replace(attr, []string{value})
	}

	if err := conn.Modify(req); err != nil {
		if ldapErr, ok := err.(*ldap.Error); ok {
			return fmt.Errorf("modify %s failed: ldap result=%d: %w",
				attr,
				ldapErr.ResultCode,
				err,
			)
		}
		return fmt.Errorf("modify %s failed: %w", attr, err)
	}

	return nil
}

func encodeUnicodePassword(password string) []byte {
	// AD の unicodePwd は、引用符で囲んだパスワードの UTF-16LE 表現を要求する。
	encoded := utf16.Encode([]rune(`"` + password + `"`))
	result := make([]byte, len(encoded)*2)
	for i, value := range encoded {
		binary.LittleEndian.PutUint16(result[i*2:], value)
	}
	return result
}

func isPasswordPolicyError(err error) bool {
	// AD DS はポリシー違反を constraintViolation または
	// unwillingToPerform で返す。接続・権限エラーはここへ含めない。
	return ldap.IsErrorAnyOf(err,
		ldap.LDAPResultConstraintViolation,
		ldap.LDAPResultUnwillingToPerform,
	)
}

func isPasswordTooYoungError(err error) bool {
	if !isPasswordPolicyError(err) {
		return false
	}

	// 0000052D は複数のパスワード制約で使われるため、コードだけでは
	// 判定せず、サーバーが返す具体的な診断文がある場合だけ専用扱いにする。
	return strings.Contains(strings.ToLower(err.Error()), "password is too young to change")
}

func newOwnPasswordModifyRequest(userDN, currentPassword, newPassword string) *ldap.ModifyRequest {
	req := ldap.NewModifyRequest(userDN, nil)
	req.Delete("unicodePwd", []string{string(encodeUnicodePassword(currentPassword))})
	req.Add("unicodePwd", []string{string(encodeUnicodePassword(newPassword))})
	return req
}

func (dao *ldapDAO) changeOwnPassword(userDN, currentPassword, newPassword string) error {
	conn, err := dao.dialLDAP()
	if err != nil {
		return err
	}
	defer conn.Close()

	// サービスアカウントではなく、本人の現在の認証情報で変更する。
	if err := conn.Bind(userDN, currentPassword); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return fmt.Errorf("%w: user bind: %w", errCurrentPasswordInvalid, err)
		}
		return fmt.Errorf("password change user bind: %w", err)
	}

	// 本人による変更は、旧値の削除と新値の追加を同じ Modify で送る。
	req := newOwnPasswordModifyRequest(userDN, currentPassword, newPassword)
	if err := conn.Modify(req); err != nil {
		if isPasswordTooYoungError(err) {
			return fmt.Errorf("%w: modify unicodePwd: %w", errPasswordTooYoung, err)
		}
		if isPasswordPolicyError(err) {
			return fmt.Errorf("%w: modify unicodePwd: %w", errPasswordPolicyViolation, err)
		}
		return fmt.Errorf("modify unicodePwd failed: %w", err)
	}

	return nil
}

const (
	adTicksPerSecond               int64 = 10_000_000
	windowsToUnixEpochOffsetSecond int64 = 11_644_473_600
)

func parseADFileTime(value string) (time.Time, error) {
	ticks, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse AD file time: %w", err)
	}
	if ticks <= 0 {
		return time.Time{}, fmt.Errorf("AD file time must be positive")
	}

	seconds := ticks/adTicksPerSecond - windowsToUnixEpochOffsetSecond
	nanoseconds := ticks % adTicksPerSecond * 100
	return time.Unix(seconds, nanoseconds).UTC(), nil
}

func parseADInterval(value string) (time.Duration, error) {
	ticks, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse AD interval: %w", err)
	}
	if ticks > 0 {
		return 0, fmt.Errorf("AD interval must be zero or negative")
	}

	// AD の Interval は負の100ナノ秒単位。time.Duration に収まらない値は拒否する。
	maxDurationTicks := int64(^uint64(0)>>1) / 100
	if ticks < -maxDurationTicks {
		return 0, fmt.Errorf("AD interval is too large")
	}

	return time.Duration(-ticks) * 100 * time.Nanosecond, nil
}

func readEntryWithConn(conn *ldap.Conn, dn string, attributes []string) (*ldap.Entry, error) {
	req := ldap.NewSearchRequest(
		dn,
		ldap.ScopeBaseObject,
		ldap.NeverDerefAliases,
		1,
		5,
		false,
		"(objectClass=*)",
		attributes,
		nil,
	)

	res, err := conn.Search(req)
	if err != nil {
		return nil, err
	}
	if len(res.Entries) != 1 {
		return nil, fmt.Errorf("expected one entry, got %d", len(res.Entries))
	}

	return res.Entries[0], nil
}

func (dao *ldapDAO) passwordChangeAllowedAt(userDN string) (time.Time, error) {
	conn, err := dao.dialLDAP()
	if err != nil {
		return time.Time{}, err
	}
	defer conn.Close()

	if err := conn.Bind(dao.config.BindDN, dao.config.BindPassword); err != nil {
		return time.Time{}, fmt.Errorf("service bind: %w", err)
	}

	user, err := readEntryWithConn(conn, userDN, []string{"pwdLastSet", "msDS-ResultantPSO"})
	if err != nil {
		return time.Time{}, fmt.Errorf("read password timestamps: %w", err)
	}
	lastSet, err := parseADFileTime(user.GetAttributeValue("pwdLastSet"))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse pwdLastSet: %w", err)
	}

	var minimumAgeValue string
	if psoDN := user.GetAttributeValue("msDS-ResultantPSO"); psoDN != "" {
		pso, err := readEntryWithConn(conn, psoDN, []string{"msDS-MinimumPasswordAge"})
		if err != nil {
			return time.Time{}, fmt.Errorf("read resultant password policy: %w", err)
		}
		minimumAgeValue = pso.GetAttributeValue("msDS-MinimumPasswordAge")
	} else {
		rootDSE, err := readEntryWithConn(conn, "", []string{"defaultNamingContext"})
		if err != nil {
			return time.Time{}, fmt.Errorf("read RootDSE: %w", err)
		}
		domainDN := rootDSE.GetAttributeValue("defaultNamingContext")
		if domainDN == "" {
			return time.Time{}, fmt.Errorf("RootDSE has no defaultNamingContext")
		}

		domain, err := readEntryWithConn(conn, domainDN, []string{"minPwdAge"})
		if err != nil {
			return time.Time{}, fmt.Errorf("read domain password policy: %w", err)
		}
		minimumAgeValue = domain.GetAttributeValue("minPwdAge")
	}

	minimumAge, err := parseADInterval(minimumAgeValue)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse minimum password age: %w", err)
	}

	return lastSet.Add(minimumAge), nil
}

func (dao *ldapDAO) readEntry(dn string, attributes []string) (*ldap.Entry, error) {
	conn, err := dao.dialLDAP()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := conn.Bind(dao.config.BindDN, dao.config.BindPassword); err != nil {
		return nil, fmt.Errorf("service bind: %w", err)
	}

	e, err := readEntryWithConn(conn, dn, attributes)
	if err != nil {
		return nil, fmt.Errorf("read entry: %w", err)
	}

	return e, nil
}

func isLDAPNoSuchObject(err error) bool {
	return ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject)
}

func (dao *ldapDAO) modifyStringAttribute(
	userDN string,
	attr string,
	current string,
	newValue string,
) (bool, error) {
	current = strings.TrimSpace(current)
	newValue = strings.TrimSpace(newValue)

	if current == newValue {
		return false, nil
	}

	if err := dao.replaceOrDeleteLDAPAttribute(userDN, attr, newValue); err != nil {
		return false, err
	}

	return true, nil
}

func (dao *ldapDAO) modifyBinaryAttribute(
	userDN string,
	attr string,
	newValue []byte,
) (bool, error) {
	if err := dao.replaceOrDeleteLDAPBinaryAttribute(userDN, attr, newValue); err != nil {
		return false, err
	}

	return true, nil
}
