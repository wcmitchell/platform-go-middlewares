package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

type identityKey int

// Internal is the "internal" field of an XRHID
type Internal struct {
	OrgID       string  `json:"org_id"`
	AuthTime    float32 `json:"auth_time,omitempty"`
	CrossAccess bool    `json:"cross_access,omitempty"`
}

// User is the "user" field of an XRHID
type User struct {
	Username  string `json:"username"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Active    bool   `json:"is_active"`
	OrgAdmin  bool   `json:"is_org_admin"`
	Internal  bool   `json:"is_internal"`
	Locale    string `json:"locale"`
	UserID    string `json:"user_id"`
}

// Associate is the "associate" field of an XRHID
type Associate struct {
	Role      []string `json:"Role"`
	Email     string   `json:"email"`
	GivenName string   `json:"givenName"`
	RHatUUID  string   `json:"rhatUUID"`
	Surname   string   `json:"surname"`
}

// X509 is the "x509" field of an XRHID
type X509 struct {
	SubjectDN string `json:"subject_dn"`
	IssuerDN  string `json:"issuer_dn"`
}

// System is the "system" field of an XRHID
type System struct {
	CommonName string `json:"cn,omitempty"`
	CertType   string `json:"cert_type,omitempty"`
	ClusterId  string `json:"cluster_id,omitempty"`
}

// Identity is the main body of the XRHID
type Identity struct {
	AccountNumber         string    `json:"account_number,omitempty"`
	EmployeeAccountNumber string    `json:"employee_account_number,omitempty"`
	OrgID                 string    `json:"org_id"`
	Internal              Internal  `json:"internal"`
	User                  User      `json:"user,omitempty"`
	System                System    `json:"system,omitempty"`
	Associate             Associate `json:"associate,omitempty"`
	X509                  X509      `json:"x509,omitempty"`
	Type                  string    `json:"type"`
	AuthType              string    `json:"auth_type"`
}

// XRHID is the "identity" pricipal object set by Cloud Platform 3scale
type XRHID struct {
	Identity Identity `json:"identity"`
}

// Key the key for the XRHID in that gets added to the context
const Key identityKey = iota

func getErrorText(code int, reason string) string {
	return http.StatusText(code) + ": " + reason
}

func doError(w http.ResponseWriter, code int, reason string) error {
	http.Error(w, getErrorText(code, reason), code)
	return errors.New(reason)
}

// Get returns the identity struct from the context
func Get(ctx context.Context) XRHID {
	return ctx.Value(Key).(XRHID)
}

// GetIdentityHeader returns the identity header from the given context if one is present.
// Can be used to retrieve the header and pass it forward to other applications.
// Returns the empty string if identity headers cannot be found.
func GetIdentityHeader(ctx context.Context) string {
	if xrhid, ok := ctx.Value(Key).(XRHID); ok {
		identityHeaders, err := json.Marshal(xrhid)
		if err != nil {
			return ""
		}
		return base64.StdEncoding.EncodeToString(identityHeaders)
	}
	return ""
}

func checkHeader(id *XRHID, w http.ResponseWriter) error {

	if (id.Identity.Type == "Associate" || id.Identity.Type == "X509") && id.Identity.AccountNumber == "" {
		return nil
	}

	if id.Identity.AccountNumber == "-1" {
		return doError(w, 400, "x-rh-identity header has an invalid account number")
	}

	if id.Identity.OrgID == "" || id.Identity.Internal.OrgID == "" {
		return doError(w, 400, "x-rh-identity header has an invalid or missing org_id")
	}

	if id.Identity.Type == "" {
		return doError(w, 400, "x-rh-identity header is missing type")
	}

	return nil
}

// EnforceIdentity extracts the X-Rh-Identity header and places the contents into the
// request context.  If the Identity is invalid, the request will be aborted.
func EnforceIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawHeaders := r.Header["X-Rh-Identity"]

		// must have an x-rh-id header
		if len(rawHeaders) != 1 {
			doError(w, 400, "missing x-rh-identity header")
			return
		}

		// must be able to base64 decode header
		idRaw, err := base64.StdEncoding.DecodeString(rawHeaders[0])
		if err != nil {
			doError(w, 400, "unable to b64 decode x-rh-identity header")
			return
		}

		var jsonData XRHID
		err = json.Unmarshal(idRaw, &jsonData)
		if err != nil {
			log.Printf("unable to unmarshal x-rh-identity header: %s", err)
			doError(w, 400, "x-rh-identity header does not contain valid JSON")
			return
		}

		topLevelOrgIDFallback(&jsonData)

		err = checkHeader(&jsonData, w)
		if err != nil {
			return
		}

		ctx := context.WithValue(r.Context(), Key, jsonData)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// if org_id is not defined at the top level, use the internal one
// https://issues.redhat.com/browse/RHCLOUD-17717
func topLevelOrgIDFallback(identity *XRHID) {
	if identity.Identity.OrgID == "" && identity.Identity.Internal.OrgID != "" {
		identity.Identity.OrgID = identity.Identity.Internal.OrgID
	}
}
