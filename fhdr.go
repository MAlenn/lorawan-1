package lorawan

import (
	"encoding/binary"
	"errors"
)

// DevAddr represents the device address.
type DevAddr uint32

// MarshalBinary marshals the object in binary form.
func (a DevAddr) MarshalBinary() ([]byte, error) {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(a))
	return b, nil
}

// UnmarshalBinary decodes the object from binary form.
func (a *DevAddr) UnmarshalBinary(data []byte) error {
	if len(data) != 4 {
		return errors.New("lorawan: 4 bytes of data are expected")
	}
	*a = DevAddr(binary.LittleEndian.Uint32(data))
	return nil
}

// FCtrl represents the FCtrl (frame control) field.
type FCtrl struct {
	ADR       bool
	ADRACKReq bool
	ACK       bool
	FPending  bool  // only used for downlink messages
	fOptsLen  uint8 // will be set automatically by the FHDR when serialized to []byte
}

// MarshalBinary marshals the object in binary form.
func (c FCtrl) MarshalBinary() ([]byte, error) {
	if c.fOptsLen > 15 {
		return []byte{}, errors.New("lorawan: max value of FOptsLen is 15")
	}
	b := byte(c.fOptsLen)
	if c.FPending {
		b = b ^ (1 << 4)
	}
	if c.ACK {
		b = b ^ (1 << 5)
	}
	if c.ADRACKReq {
		b = b ^ (1 << 6)
	}
	if c.ADR {
		b = b ^ (1 << 7)
	}
	return []byte{b}, nil
}

// UnmarshalBinary decodes the object from binary form.
func (c *FCtrl) UnmarshalBinary(data []byte) error {
	if len(data) != 1 {
		return errors.New("lorawan: 1 byte of data is expected")
	}
	c.fOptsLen = data[0] & ((1 << 3) ^ (1 << 2) ^ (1 << 1) ^ (1 << 0))
	c.FPending = data[0]&(1<<4) > 0
	c.ACK = data[0]&(1<<5) > 0
	c.ADRACKReq = data[0]&(1<<6) > 0
	c.ADR = data[0]&(1<<7) > 0
	return nil
}

// FHDR represents the frame header.
type FHDR struct {
	DevAddr DevAddr
	FCtrl   FCtrl
	Fcnt    uint16
	FOpts   []MACCommand // max. number of allowed bytes is 15
	uplink  bool         // used for the (un)marshaling, not part of the spec.
}

// MarshalBinary marshals the object in binary form.
func (h FHDR) MarshalBinary() ([]byte, error) {
	var b []byte
	var err error
	opts := make([]byte, 0)
	for _, mac := range h.FOpts {
		mac.uplink = h.uplink
		b, err = mac.MarshalBinary()
		if err != nil {
			return []byte{}, err
		}
		opts = append(opts, b...)
	}
	h.FCtrl.fOptsLen = uint8(len(opts))
	if h.FCtrl.fOptsLen > 15 {
		return []byte{}, errors.New("lorawan: max number of FOpts bytes is 15")
	}

	out := make([]byte, 0, 7+h.FCtrl.fOptsLen)
	b, err = h.DevAddr.MarshalBinary()
	if err != nil {
		return []byte{}, err
	}
	out = append(out, b...)

	b, err = h.FCtrl.MarshalBinary()
	if err != nil {
		return []byte{}, err
	}
	out = append(out, b...)
	out = append(out, []byte{0, 0}...) // used by PutUint16
	binary.LittleEndian.PutUint16(out[5:7], h.Fcnt)
	out = append(out, opts...)

	return out, nil
}

func (h *FHDR) UnmarshalBinary(data []byte) error {
	if len(data) < 7 {
		return errors.New("lorawan: at least 7 bytes are expected")
	}

	if err := h.DevAddr.UnmarshalBinary(data[0:4]); err != nil {
		return err
	}
	if err := h.FCtrl.UnmarshalBinary(data[4:5]); err != nil {
		return err
	}
	h.Fcnt = binary.LittleEndian.Uint16(data[5:7])

	if len(data) > 7 {
		var pLen int
		for i := 0; i < len(data[7:]); i++ {
			switch {
			case !h.uplink && cid(data[7+i]) == LinkCheckAns:
				pLen = 2
			case !h.uplink && cid(data[7+i]) == LinkADRReq:
				pLen = 4
			case h.uplink && cid(data[7+i]) == LinkADRAns:
				pLen = 1
			case !h.uplink && cid(data[7+i]) == DutyCycleReq:
				pLen = 1
			case !h.uplink && cid(data[7+i]) == RXParamSetupReq:
				pLen = 4
			case h.uplink && cid(data[7+i]) == RXParamSetupAns:
				pLen = 1
			case h.uplink && cid(data[7+i]) == DevStatusAns:
				pLen = 2
			case !h.uplink && cid(data[7+i]) == NewChannelReq:
				pLen = 4
			case h.uplink && cid(data[7+i]) == NewChannelAns:
				pLen = 1
			case !h.uplink && cid(data[7+i]) == RXTimingSetupReq:
				pLen = 1
			default:
				pLen = 0 // the MAC command does not have a payload
			}
			// TODO handle proprietary payload

			// check if the remaining bytes are >= CID byte + payload size
			if len(data[7+i:]) < pLen+1 {
				return errors.New("lorawan: not enough remaining bytes")
			}

			mc := MACCommand{uplink: h.uplink} // MACCommand needs to know if the msg is uplink or downlink
			if err := mc.UnmarshalBinary(data[7+i : 7+i+1+pLen]); err != nil {
				return err
			}
			h.FOpts = append(h.FOpts, mc)

			// go to the next command (skip the payload bytes of the current command)
			i = i + pLen
		}
	}

	return nil
}
