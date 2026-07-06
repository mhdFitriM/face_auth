package internal

// event_decoder.go — Maps Hikvision ISAPI major/minor event codes and
// subStatusCode error codes to human-readable descriptions.
//
// Usage:
//   decoded := DecodeEvent(5, 75)       // Face Authentication Completed
//   errInfo := DecodeISAPIError("employeeNoAlreadyExist", 0)

import "fmt"

// ── Event decoder ─────────────────────────────────────────────────────────────

// DecodedEvent is the normalized output from DecodeEvent.
type DecodedEvent struct {
	Major     int    `json:"major"`
	MajorHex  string `json:"majorHex"`
	MajorName string `json:"majorName"`
	Minor     int    `json:"minor"`
	MinorHex  string `json:"minorHex"`
	Message   string `json:"message"`
}

// DecodeEvent converts a major/minor pair into a human-readable DecodedEvent.
// major and minor are decimal values as returned by the device AcsEvent API.
func DecodeEvent(major, minor int) DecodedEvent {
	return DecodedEvent{
		Major:     major,
		MajorHex:  fmt.Sprintf("0x%x", major),
		MajorName: decodeMajorName(major),
		Minor:     minor,
		MinorHex:  fmt.Sprintf("0x%x", minor),
		Message:   decodeMinorMessage(major, minor),
	}
}

func decodeMajorName(major int) string {
	switch major {
	case 1:
		return "Alarm Events"
	case 2:
		return "Exception Events"
	case 3:
		return "Operation Events"
	case 4:
		return "Additional Information"
	case 5:
		return "Other Events"
	}
	return fmt.Sprintf("Unknown (0x%x)", major)
}

func decodeMinorMessage(major, minor int) string {
	key := (major << 16) | minor
	if msg, ok := eventCodeMap[key]; ok {
		return msg
	}
	return fmt.Sprintf("Unknown event (major=0x%x minor=0x%x / %d)", major, minor, minor)
}

// eventCodeMap maps (major<<16 | minor) → human-readable description.
// Minor values are decimal (hex shown in comments for reference).
var eventCodeMap = map[int]string{
	// ── Major 5: Other Events ────────────────────────────────────────────────
	encode(5, 1):    "Valid Card Authentication Completed",         // 0x01
	encode(5, 2):    "Card and Password Authentication Completed",  // 0x02
	encode(5, 3):    "Card and Password Authentication Failed",     // 0x03
	encode(5, 4):    "Card and Password Authentication Timed Out",  // 0x04
	encode(5, 21):   "Door Unlocked",                               // 0x15
	encode(5, 22):   "Door Locked",                                 // 0x16
	encode(5, 23):   "Exit Button Pressed",                         // 0x17
	encode(5, 24):   "Exit Button Released",                        // 0x18
	encode(5, 25):   "Door Open Contact",                           // 0x19
	encode(5, 26):   "Door Closed Contact",                         // 0x1a
	encode(5, 54):   "Access Granted by Face and Fingerprint",      // 0x36
	encode(5, 55):   "Access Denied by Face and Fingerprint",       // 0x37
	encode(5, 56):   "Access Timed Out by Face and Fingerprint",    // 0x38
	encode(5, 57):   "Access Granted by Face and PIN",              // 0x39
	encode(5, 58):   "Access Denied by Face and PIN",               // 0x3a
	encode(5, 59):   "Access Timed Out by Face and PIN",            // 0x3b
	encode(5, 60):   "Access Granted by Face and Card",             // 0x3c
	encode(5, 61):   "Access Denied by Face and Card",              // 0x3d
	encode(5, 62):   "Access Timed Out by Face and Card",           // 0x3e
	encode(5, 63):   "Access Granted by Face, PIN, and Fingerprint", // 0x3f
	encode(5, 64):   "Access Denied by Face, PIN, and Fingerprint",  // 0x40
	encode(5, 65):   "Access Timed Out by Face, PIN, and Fingerprint", // 0x41
	encode(5, 66):   "Access Granted by Face, Card, and Fingerprint", // 0x42
	encode(5, 67):   "Access Denied by Face, Card, and Fingerprint",  // 0x43
	encode(5, 68):   "Access Timed Out by Face, Card, and Fingerprint", // 0x44
	encode(5, 75):   "Face Authentication Completed",               // 0x4b
	encode(5, 76):   "Face Authentication Failed",                  // 0x4c
	encode(5, 77):   "Employee ID and Face Authentication Completed", // 0x4d
	encode(5, 78):   "Employee ID and Face Authentication Failed",    // 0x4e
	encode(5, 79):   "Employee ID and Face Authentication Timed Out", // 0x4f
	encode(5, 80):   "Face Recognition Failed",                     // 0x50
	encode(5, 190):  "Access Granted by Iris",                      // 0xbe
	encode(5, 191):  "Access Denied via Iris",                      // 0xbf
	encode(5, 202):  "Authentication Failed: Fingerprint Recognition Not Enabled", // 0xca
	encode(5, 203):  "Authentication Failed: Face Capture Disabled", // 0xcb
	encode(5, 208):  "Authentication Failed Due to Abnormal Features (mask/hard hat/abnormal temperature)", // 0xd0
	encode(5, 212):  "Person Authentication Times Reached Limit",   // 0xd4
	encode(5, 213):  "Upgrading Face Modeling Failed Due to Incompatible Version", // 0xd5
	encode(5, 218):  "Passing Event",                               // 0xda
	encode(5, 221):  "Wearing Mask",                                // 0xdd
	encode(5, 223):  "Captured Picture Too Small",                  // 0xdf
	encode(5, 226):  "Access Granted by Employee ID",               // 0xe2
	encode(5, 227):  "Access Granted by NFC",                       // 0xe3
	encode(5, 228):  "Access Denied by NFC",                        // 0xe4
	encode(5, 2063): "Invalid Face Recognition Angle",              // 0x80f

	// ── Major 2: Exception Events ────────────────────────────────────────────
	encode(2, 1062): "Face Recognition Terminal Online",            // 0x426
	encode(2, 1063): "Face Recognition Terminal Offline",           // 0x427
	encode(2, 1041): "Low Battery Voltage (Face Recognition Terminal)", // 0x411
	encode(2, 1042): "Battery Voltage Recovered (Face Recognition Terminal)", // 0x412

	// ── Major 1: Alarm Events ────────────────────────────────────────────────
	encode(1, 1043): "Low Face Picture Quality",                    // 0x413
	encode(1, 1038): "Capture Linkage Alarm",                       // 0x40e

	// ── Major 3: Operation Events ─────────────────────────────────────────────
	encode(3, 1061): "ISAPI and HTTP Login",                        // 0x425
	encode(3, 1062): "ISAPI and HTTP Logout",                       // 0x426
	encode(3, 1063): "ISAPI and HTTPS Login",                       // 0x427
	encode(3, 1064): "ISAPI and HTTPS Logout",                      // 0x428
	encode(3, 1068): "Face Picture Card Issuing Records",           // 0x42c
	encode(3, 1074): "Add Person Information",                      // 0x432
	encode(3, 1075): "Edit Person Information",                     // 0x433
	encode(3, 1076): "Delete Person Information by Employee ID",    // 0x434
	encode(3, 1083): "Enable Face Capture",                         // 0x43b
	encode(3, 1084): "Disable Face Capture",                        // 0x43c
	encode(3, 1091): "Face Picture Data Library Upgrade Log Records", // 0x443
}

func encode(major, minor int) int { return (major << 16) | minor }

// ── Error decoder ─────────────────────────────────────────────────────────────

// DecodedISAPIError is the normalized output from DecodeISAPIError.
type DecodedISAPIError struct {
	RawCode       string `json:"rawCode"`
	SubStatusCode string `json:"subStatusCode"`
	Message       string `json:"message"`
	Category      string `json:"category"`
	Suggestion    string `json:"suggestion"`
}

// ISAPIResponseStatus mirrors the common status envelope in Hikvision responses.
type ISAPIResponseStatus struct {
	StatusCode    int    `json:"statusCode"`
	StatusString  string `json:"statusString"`
	SubStatusCode string `json:"subStatusCode"`
	ErrorCode     int    `json:"errorCode"`
	ErrorMsg      string `json:"errorMsg"`
}

// IsOK returns true when the status indicates success.
func (s ISAPIResponseStatus) IsOK() bool {
	return s.StatusCode == 1 || s.SubStatusCode == "ok" || s.StatusString == "OK"
}

// Decode returns a DecodedISAPIError for this status.
func (s ISAPIResponseStatus) Decode() DecodedISAPIError {
	return DecodeISAPIError(s.SubStatusCode, s.ErrorCode)
}

// DecodeISAPIError maps a Hikvision subStatusCode (and optionally a raw
// errorCode) to a human-readable description with a category and suggestion.
func DecodeISAPIError(subStatusCode string, errorCode int) DecodedISAPIError {
	if e, ok := isapiErrorMap[subStatusCode]; ok {
		e.SubStatusCode = subStatusCode
		return e
	}
	// Fallback: look up by numeric errorCode hex string
	if errorCode != 0 {
		hex := fmt.Sprintf("0x%x", uint32(errorCode))
		if e, ok := isapiErrorByCode[hex]; ok {
			e.SubStatusCode = subStatusCode
			return e
		}
	}
	return DecodedISAPIError{
		SubStatusCode: subStatusCode,
		Message:       fmt.Sprintf("Unknown error (subStatusCode=%q errorCode=0x%x)", subStatusCode, uint32(errorCode)),
		Category:      "unknown",
	}
}

// isapiErrorMap maps subStatusCode string → partial DecodedISAPIError.
// RawCode is the hex error code for reference.
var isapiErrorMap = map[string]DecodedISAPIError{
	"ok": {
		RawCode: "0x1", Message: "Succeeded", Category: "success",
	},

	// ── Password / auth ───────────────────────────────────────────────────────
	"riskPassword": {
		RawCode: "0x10000002", Message: "Risky password", Category: "auth",
		Suggestion: "Choose a stronger password that meets device complexity requirements.",
	},
	"illegalPassword": {
		RawCode: "0x10000004", Message: "Invalid password; password cannot contain username", Category: "auth",
		Suggestion: "Ensure the password does not include the username as a substring.",
	},
	"badAuthorization": {
		RawCode: "0x40000003", Message: "Authentication failed", Category: "auth",
		Suggestion: "Check Digest Auth credentials (username/password). Ensure HTTPS is used if required.",
	},
	"loginPasswordError": {
		RawCode: "0x4000000C", Message: "Incorrect login password", Category: "auth",
		Suggestion: "Verify credentials. Account may be locked after repeated failures.",
	},
	"incorrentUserNameOrPassword": {
		RawCode: "0x60000012", Message: "Incorrect username or password", Category: "auth",
		Suggestion: "Re-check ISAPI username/password in device settings.",
	},
	"terminalPasswordError": {
		RawCode: "0x40005126", Message: "Incorrect terminal username or password", Category: "auth",
		Suggestion: "Check ISAPIUsername and ISAPIPassword in device configuration.",
	},
	"terminalNotActivated": {
		RawCode: "0x40005128", Message: "Terminal is not activated", Category: "activation",
		Suggestion: "Activate the device via PUT /ISAPI/System/activate before making any calls.",
	},
	"notActivated": {
		RawCode: "0x40000007", Message: "Device not activated", Category: "activation",
		Suggestion: "Activate the device first.",
	},

	// ── Person / user ─────────────────────────────────────────────────────────
	"userInfoIsEmpty": {
		RawCode: "0x40005114", Message: "User data is required", Category: "person",
		Suggestion: "Ensure employeeNo and name are provided in the UserInfo payload.",
	},
	"errorUserInfo": {
		RawCode: "0x4000a009", Message: "Incorrect person information", Category: "person",
		Suggestion: "Check field types, lengths, and allowed character sets (employeeNo must be alphanumeric).",
	},
	"employeeNoNotExist": {
		RawCode: "0x6000601f", Message: "Employee No. does not exist", Category: "person",
		Suggestion: "Create the person with createPerson() before modifying or assigning a face.",
	},
	"employeeNoAlreadyExist": {
		RawCode: "0x60006020", Message: "Employee No. already exists", Category: "person",
		Suggestion: "Use updatePerson() instead of createPerson(), or choose a different employeeNo.",
	},
	"notSupportCharacter": {
		RawCode: "0x60006029", Message: "Invalid characters in field", Category: "person",
		Suggestion: "employeeNo and FPID must be alphanumeric only [A-Za-z0-9]. Strip hyphens, underscores, and spaces.",
	},
	"illegalEmployeeNo": {
		RawCode: "0x60006036", Message: "Incorrect employee No.", Category: "person",
		Suggestion: "Ensure employeeNo contains only [A-Za-z0-9] and is within the allowed length (≤32 chars).",
	},
	"deviceUserFull": {
		RawCode: "0x60007000", Message: "No more people are allowed", Category: "person",
		Suggestion: "Delete unused persons from the device. Check device person capacity in capabilities.",
	},
	"deviceUserAlreadyExist": {
		RawCode: "0x60007002", Message: "Person already exists", Category: "person",
		Suggestion: "Use updatePerson() instead of createPerson().",
	},
	"deviceUserNotExist": {
		RawCode: "0x60007003", Message: "Person does not exist", Category: "person",
		Suggestion: "Create the person first with createPerson().",
	},
	"deviceUserAlreadyExistFace": {
		RawCode: "0x60007004", Message: "Person already has a face picture", Category: "face",
		Suggestion: "Delete the existing face first with deleteFace(), then upload the new one.",
	},
	"uploadUserSuperCaps": {
		RawCode: "0x400010D7", Message: "No more user information can be uploaded", Category: "person",
		Suggestion: "Device user capacity reached. Delete unused persons or check device limits.",
	},

	// ── Face / picture ────────────────────────────────────────────────────────
	"pictureUploadFailed": {
		RawCode: "0x3000000B", Message: "Uploading picture failed", Category: "face",
		Suggestion: "Check image format (JPG required), size (<5 MB), and resolution (<4096×4096).",
	},
	"pictureSizeExceedLimit": {
		RawCode: "0x3000000C", Message: "Picture size exceeded limit", Category: "face",
		Suggestion: "Reduce image file size to below 5 MB.",
	},
	"improperResolution": {
		RawCode: "0x30002025", Message: "Picture resolution exceeded limit (4096×4096)", Category: "face",
		Suggestion: "Resize the image to within 4096×4096 pixels.",
	},
	"improperPicSize": {
		RawCode: "0x30002026", Message: "Picture file size exceeded limit (5 MB)", Category: "face",
		Suggestion: "Compress or resize the image to below 5 MB.",
	},
	"URLDownloadFailed": {
		RawCode: "0x30002027", Message: "Downloading picture from URL failed", Category: "face",
		Suggestion: "Use direct upload (multipart/form-data) instead of URL-based upload.",
	},
	"unsupportedImageFormat": {
		RawCode: "0x30002028", Message: "Invalid uploaded picture format; upload JPG", Category: "face",
		Suggestion: "Convert the image to JPEG format before uploading.",
	},
	"lowScore": {
		RawCode: "0x30006001", Message: "Low quality of collected face data", Category: "face",
		Suggestion: "Use a clearer, well-lit, front-facing photo. Avoid masks, helmets, or side angles.",
	},
	"fileSizeExceedError": {
		RawCode: "0x30006010", Message: "Applying face picture failed: file size exceeded", Category: "face",
		Suggestion: "Reduce face image file size. Device has a per-image size limit.",
	},
	"deletePictureError": {
		RawCode: "0x3000000A", Message: "Deleting picture failed", Category: "face",
		Suggestion: "Ensure the FPID/FDID is correct and the face record exists on the device.",
	},
	"facePictureModelInvalid": {
		RawCode: "0x4000109B", Message: "Invalid face model", Category: "face",
		Suggestion: "Re-upload a higher quality face image. The previous modeling failed.",
	},
	"getHumanFaceFrameFailed": {
		RawCode: "0x4000109F", Message: "Getting face frame from picture failed", Category: "face",
		Suggestion: "No face detected in the uploaded image. Ensure one clear, front-facing face is present.",
	},
	"modelingFailed": {
		RawCode: "0x400010A0", Message: "Face modeling from picture URL failed", Category: "face",
		Suggestion: "Use direct multipart upload instead of URL. Ensure JPG with a single clear face.",
	},
	"pictureModelingFailed": {
		RawCode: "0x40001101", Message: "Picture modeling failed", Category: "face",
		Suggestion: "Re-capture or re-scan the face image. Ensure: front-facing, well-lit, no obstruction.",
	},
	"addFaceRecordFailed": {
		RawCode: "0x400010C1", Message: "Adding face record failed", Category: "face",
		Suggestion: "Check that FDID, faceLibType, and FPID are valid. Confirm face library exists.",
	},
	"deleteFaceRecordFailed": {
		RawCode: "0x400010C2", Message: "Deleting face record failed", Category: "face",
		Suggestion: "Verify FPID/FDID. The face record may not exist.",
	},
	"modifyFaceRecordFailed": {
		RawCode: "0x400010C3", Message: "Editing face record failed", Category: "face",
		Suggestion: "Ensure FPID and FDID match an existing record.",
	},
	"queryFaceRecordFailed": {
		RawCode: "0x400010C4", Message: "Searching face records failed", Category: "face",
		Suggestion: "Check that the face library (FDID) exists and is of the correct faceLibType.",
	},
	"faceDetectFailed": {
		RawCode: "0x400010C5", Message: "Face detection failed", Category: "face",
		Suggestion: "Ensure the image contains exactly one visible, unobstructed, front-facing face.",
	},
	"libraryNotExist": {
		RawCode: "0x400010C6", Message: "The face library does not exist", Category: "face",
		Suggestion: "Create the face library first with ensureFaceLibrary() before uploading faces.",
	},
	"faceDataPIDError": {
		RawCode: "0x60000030", Message: "Invalid face data PID", Category: "face",
		Suggestion: "FPID must be alphanumeric [A-Za-z0-9], max 32 chars. Strip hyphens and special chars.",
	},
	"faceDataNotExist": {
		RawCode: "0x40006012", Message: "User facial data does not exist", Category: "face",
		Suggestion: "Upload the face image first with uploadFace() before assigning permissions.",
	},
	"alreadyExistThisFace": {
		RawCode: "0x4000a008", Message: "The face picture already exists", Category: "face",
		Suggestion: "Delete the existing face record first, then re-upload.",
	},

	// ── Device / support ─────────────────────────────────────────────────────
	"notSupport": {
		RawCode: "0x40000001", Message: "Not supported", Category: "device",
		Suggestion: "This feature is not available on this device/firmware. Run checkDeviceFunctionSupport() to confirm.",
	},
	"lowPrivilege": {
		RawCode: "0x40000002", Message: "No permission", Category: "auth",
		Suggestion: "Use admin credentials or an account with the required permission level.",
	},
	"methodNotAllowed": {
		RawCode: "0x40000004", Message: "Invalid HTTP method", Category: "device",
		Suggestion: "Check the HTTP method (GET/POST/PUT/DELETE) expected by the endpoint.",
	},
	"invalidContent": {
		RawCode: "0x4000000A", Message: "Invalid message body", Category: "request",
		Suggestion: "Validate the JSON payload against the device's expected schema. Check required fields.",
	},
	"IDNotexist": {
		RawCode: "0x40001001", Message: "ID does not exist", Category: "device",
		Suggestion: "The referenced ID (FDID, FPID, plan ID, etc.) does not exist on the device.",
	},
	"invalidID": {
		RawCode: "0x40001008", Message: "Invalid ID", Category: "request",
		Suggestion: "Check ID format and value. IDs must be numeric or alphanumeric per endpoint rules.",
	},
	"IDAlreadyExists": {
		RawCode: "0x4000100F", Message: "ID No. already exists", Category: "device",
		Suggestion: "The entity with this ID already exists. Use modify/update endpoint instead.",
	},
	"noRecord": {
		RawCode: "0x4000109D", Message: "No record found", Category: "device",
		Suggestion: "No matching records for the search criteria. Adjust filters or confirm data exists.",
	},
	"eventNotSupport": {
		RawCode: "0x60001024", Message: "Event subscription is not supported", Category: "device",
		Suggestion: "Use historical AcsEvent search instead. Real-time alertStream may still work.",
	},
	"diskError": {
		RawCode: "", Message: "HDD/storage error on device", Category: "device",
		Suggestion: "Check device storage health. Face modeling may fail if storage is unavailable.",
	},
	"diskFull": {
		RawCode: "", Message: "Device storage is full", Category: "device",
		Suggestion: "Free up device storage by removing old event logs or face images.",
	},
}

// isapiErrorByCode maps hex error code string → partial DecodedISAPIError (fallback lookup).
var isapiErrorByCode = map[string]DecodedISAPIError{
	"0x1":        {RawCode: "0x1", Message: "Succeeded", Category: "success"},
	"0x40000001": isapiErrorMap["notSupport"],
	"0x40000003": isapiErrorMap["badAuthorization"],
	"0x40000007": isapiErrorMap["notActivated"],
	"0x60007002": isapiErrorMap["deviceUserAlreadyExist"],
	"0x60007003": isapiErrorMap["deviceUserNotExist"],
	"0x60006020": isapiErrorMap["employeeNoAlreadyExist"],
	"0x60006029": isapiErrorMap["notSupportCharacter"],
	"0x400010c6": isapiErrorMap["libraryNotExist"],
	"0x400010c1": isapiErrorMap["addFaceRecordFailed"],
	"0x4000a008": isapiErrorMap["alreadyExistThisFace"],
	"0x30002028": isapiErrorMap["unsupportedImageFormat"],
	"0x3000000c": isapiErrorMap["pictureSizeExceedLimit"],
}
