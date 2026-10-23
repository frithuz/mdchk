package parser

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The factual scheme identifiers below freeze the adopted reader's URI set
// (Text/Pandoc/URI.hs at e51c9c6054c8f4ec5c3209d5abe10939dfe2963e).
// They are syntax data, not a network allowlist or a public inline provider.
const pandocHeadingSchemes = "aaa aaas about acap acct acr adiumxtra afp afs aim appdata apt attachment aw barion beshare bitcoin " +
	"blob bolo browserext callto cap chrome chrome-extension cid coap coaps com-eventbrite-attendee " +
	"content crid cvs data dav dict dis dlna-playcontainer dlna-playsingle dns dntp dtn dvb ed2k example " +
	"facetime fax feed feedready file filesystem finger fish ftp geo gg git gizmoproject go gopher graph " +
	"gtalk h323 ham hcp http https hxxp hxxps hydrazone iax icap icon im imap info iotdisco ipn ipp ipps " +
	"irc irc6 ircs iris iris.beep iris.lwz iris.xpc iris.xpcs isostore itms jabber jar jms keyparc lastfm " +
	"ldap ldaps lvlt magnet mailserver mailto maps market message mid mms modem mongodb moz ms-access " +
	"ms-browser-extension ms-drive-to ms-enrollment ms-excel ms-gamebarservices ms-getoffice ms-help " +
	"ms-infopath ms-media-stream-id ms-officeapp ms-project ms-powerpoint ms-publisher ms-search-repair " +
	"ms-secondary-screen-controller ms-secondary-screen-setup ms-settings ms-settings-airplanemode " +
	"ms-settings-bluetooth ms-settings-camera ms-settings-cellular ms-settings-cloudstorage " +
	"ms-settings-connectabledevices ms-settings-displays-topology ms-settings-emailandaccounts " +
	"ms-settings-language ms-settings-location ms-settings-lock ms-settings-nfctransactions " +
	"ms-settings-notifications ms-settings-power ms-settings-privacy ms-settings-proximity " +
	"ms-settings-screenrotation ms-settings-wifi ms-settings-workplace ms-spd ms-sttoverlay ms-transit-to " +
	"ms-virtualtouchpad ms-visio ms-walk-to ms-whiteboard ms-whiteboard-cmd ms-word msnim msrp msrps mtqp " +
	"mumble mupdate mvn news nfs ni nih nntp notes ocf oid onenote onenote-cmd opaquelocktoken pack palm " +
	"paparazzi pkcs11 platform pop pres prospero proxy pwid psyc qb query redis rediss reload res " +
	"resource rmi rsync rtmfp rtmp rtsp rtsps rtspu secondlife service session sftp sgn shttp sieve sip " +
	"sips skype smb sms smtp snews snmp soap.beep soap.beeps soldat spotify ssh steam stun stuns submit " +
	"svn tag teamspeak tel teliaeid telnet tftp things thismessage tip tn3270 tool turn turns tv udp " +
	"unreal urn ut2004 v-event vemmi ventrilo videotex vnc view-source wais webcal wpid ws wss wtai " +
	"wyciwyg xcon xcon-userid xfire xmlrpc.beep xmlrpc.beeps xmpp xri ymsgr z39.50 z39.50r z39.50s doi " +
	"gemini isbn javascript pmid "

// A selected autolink must first recognize a URI/email prefix. Its consumer
// then owns the remaining nonspace bytes through '>', before attached attrs.
func pandocHeadingAutolinkEnd(ctx context.Context, text string, start, end int) int {
	if end == 0 {
		return start
	}
	inner := text[start+1 : end]
	if pandocHeadingEmailPrefix(ctx, inner) {
		return end + 1
	}
	colon := 0
	for colon < len(inner) && inner[colon] != ':' {
		meterWork(ctx, 1)
		c := inner[colon]
		if !asciiAlphanumeric(c) && c != '+' && c != '-' && c != '.' {
			return start
		}
		colon++
	}
	if colon <= 0 || colon+1 >= len(inner) {
		return start
	}
	meterWork(ctx, len(pandocHeadingSchemes)+colon*2)
	scheme := strings.ToLower(inner[:colon])
	if !strings.Contains(" "+pandocHeadingSchemes, " "+scheme+" ") {
		return start
	}
	body := inner[colon+1:]
	if body[0] == '*' || body[0] == '_' || body[0] == ']' {
		return start
	}
	first := pandocHeadingURIChunk(ctx, body, 0)
	if strings.ContainsRune("({[", rune(body[0])) && len(body) > 1 {
		closing := byte(')')
		if body[0] == '{' {
			closing = '}'
		} else if body[0] == '[' {
			closing = ']'
		}
		inside := pandocHeadingURIChunk(ctx, body, 1)
		if inside > 1 && inside < len(body) && body[inside] == closing {
			return end + 1
		}
	}
	if first > 0 {
		return end + 1
	}
	return start
}
func pandocHeadingURIWord(r rune) bool {
	return attributeAlphaNum(r) || strings.ContainsRune("#$%+/@\\_-&=", r)
}
func pandocHeadingURIChunk(ctx context.Context, text string, start int) int {
	if start == len(text) {
		return start
	}
	r, n := utf8.DecodeRuneInString(text[start:])
	meterWork(ctx, 1)
	if pandocHeadingURIWord(r) {
		i := start + n
		for i < len(text) {
			meterWork(ctx, 1)
			r, n = utf8.DecodeRuneInString(text[i:])
			if !pandocHeadingURIWord(r) {
				break
			}
			i += n
		}
		return i
	}
	if unicode.IsSpace(r) || r == '<' || r == '>' {
		return start
	}
	i := start + n
	if r == ',' {
		for i < len(text) && text[i] == ',' {
			meterWork(ctx, 1)
			i++
		}
	}
	if i < len(text) {
		r, _ = utf8.DecodeRuneInString(text[i:])
		meterWork(ctx, 1)
		if pandocHeadingURIWord(r) {
			return i
		}
	}
	return start
}
func pandocHeadingEmailPrefix(ctx context.Context, text string) bool {
	i := 0
	for {
		if i == len(text) {
			return false
		}
		r, n := utf8.DecodeRuneInString(text[i:])
		meterWork(ctx, 1)
		if !attributeAlphaNum(r) {
			return false
		}
		i += n
		for i < len(text) {
			meterWork(ctx, 1)
			r, n = utf8.DecodeRuneInString(text[i:])
			if !attributeAlphaNum(r) && !strings.ContainsRune("!\"#$%&'*+-/=?^_{|}~;", r) {
				break
			}
			i += n
		}
		if i == len(text) {
			return false
		}
		if text[i] == '.' {
			i++
			continue
		}
		if text[i] != '@' {
			return false
		}
		i++
		break
	}
	if i == len(text) {
		return false
	}
	r, n := utf8.DecodeRuneInString(text[i:])
	meterWork(ctx, 1)
	if attributeAlphaNum(r) {
		return true
	}
	if r == '-' && i+n < len(text) {
		r, _ = utf8.DecodeRuneInString(text[i+n:])
		meterWork(ctx, 1)
		return attributeAlphaNum(r)
	}
	return false
}
