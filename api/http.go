package api

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	goproto "google.golang.org/protobuf/proto"

	"github.com/iotexproject/pebble-server/contract/ioid"
	"github.com/iotexproject/pebble-server/contract/ioidregistry"
	"github.com/iotexproject/pebble-server/db"
	"github.com/iotexproject/pebble-server/proto"
)

type errResp struct {
	Error string `json:"error,omitempty"`
}

func newErrResp(err error) *errResp {
	return &errResp{Error: err.Error()}
}

type queryReq struct {
	DeviceID  string `json:"deviceID"                   binding:"required"`
	Signature string `json:"signature,omitempty"        binding:"required"`
}

type queryResp struct {
	Status   int32  `json:"status"`
	Owner    string `json:"owner"`
	Firmware string `json:"firmware,omitempty"`
	URI      string `json:"uri,omitempty"`
	Version  string `json:"version,omitempty"`
}

type receiveReq struct {
	DeviceID  string `json:"deviceID"                   binding:"required"`
	Payload   string `json:"payload"                    binding:"required"`
	Signature string `json:"signature,omitempty"        binding:"required"`
}

type httpServer struct {
	engine               *gin.Engine
	db                   *db.DB
	ioidInstance         *ioid.Ioid
	ioidRegistryInstance *ioidregistry.Ioidregistry
}

func (s *httpServer) query(c *gin.Context) {
	req := &queryReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		slog.Error("failed to bind request", "error", err)
		c.JSON(http.StatusBadRequest, newErrResp(errors.Wrap(err, "invalid request payload")))
		return
	}

	sigStr := req.Signature
	req.Signature = ""

	reqJson, err := json.Marshal(req)
	if err != nil {
		slog.Error("failed to marshal request into json format", "error", err)
		c.JSON(http.StatusInternalServerError, newErrResp(errors.Wrap(err, "failed to process request data")))
		return
	}

	sig, err := hexutil.Decode(sigStr)
	if err != nil {
		slog.Error("failed to decode signature from hex format", "signature", sigStr, "error", err)
		c.JSON(http.StatusBadRequest, newErrResp(errors.Wrap(err, "invalid signature format")))
		return
	}

	h := crypto.Keccak256Hash(reqJson)
	sigpk, err := crypto.SigToPub(h.Bytes(), sig)
	if err != nil {
		slog.Error("failed to recover public key from signature", "error", err)
		c.JSON(http.StatusBadRequest, newErrResp(errors.Wrap(err, "invalid signature; could not recover public key")))
		return
	}

	owner := crypto.PubkeyToAddress(*sigpk)

	d, err := s.db.Device(req.DeviceID)
	if err != nil {
		slog.Error("failed to query device", "error", err, "device_id", req.DeviceID)
		c.JSON(http.StatusInternalServerError, newErrResp(errors.Wrap(err, "failed to query device")))
		return
	}
	if d == nil {
		slog.Error("device not exist", "device_id", req.DeviceID)
		c.JSON(http.StatusBadRequest, newErrResp(errors.New("device not exist")))
		return
	}
	if d.Owner != owner.String() {
		slog.Error("no permission to access the device", "device_id", req.DeviceID)
		c.JSON(http.StatusForbidden, newErrResp(errors.New("no permission to access the device")))
		return
	}

	var (
		firmware string
		uri      string
		version  string
	)
	if parts := strings.Split(d.RealFirmware, " "); len(parts) == 2 {
		app, err := s.db.App(parts[0])
		if err != nil {
			slog.Error("failed to query app", "error", err, "app_id", parts[0])
			c.JSON(http.StatusInternalServerError, newErrResp(errors.Wrap(err, "failed to query app")))
			return
		}
		if app != nil {
			firmware = app.ID
			uri = app.Uri
			version = app.Version
		}
	}

	c.JSON(http.StatusOK, &queryResp{
		Status:   d.Status,
		Owner:    d.Owner,
		Firmware: firmware,
		URI:      uri,
		Version:  version,
	})
}

func (s *httpServer) receive(c *gin.Context) {
	req := &receiveReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		slog.Error("failed to bind request", "error", err)
		c.JSON(http.StatusBadRequest, newErrResp(errors.Wrap(err, "invalid request payload")))
		return
	}

	sigStr := req.Signature
	req.Signature = ""

	reqJson, err := json.Marshal(req)
	if err != nil {
		slog.Error("failed to marshal request into json format", "error", err)
		c.JSON(http.StatusInternalServerError, newErrResp(errors.Wrap(err, "failed to process request data")))
		return
	}

	sig, err := hexutil.Decode(sigStr)
	if err != nil {
		slog.Error("failed to decode signature from hex format", "signature", sigStr, "error", err)
		c.JSON(http.StatusBadRequest, newErrResp(errors.Wrap(err, "invalid signature format")))
		return
	}

	h := crypto.Keccak256Hash(reqJson)
	sigpk, err := crypto.SigToPub(h.Bytes(), sig)
	if err != nil {
		slog.Error("failed to recover public key from signature", "error", err)
		c.JSON(http.StatusBadRequest, newErrResp(errors.Wrap(err, "invalid signature; could not recover public key")))
		return
	}

	owner := crypto.PubkeyToAddress(*sigpk)

	d, err := s.db.Device(req.DeviceID)
	if err != nil {
		slog.Error("failed to query device", "error", err, "device_id", req.DeviceID)
		c.JSON(http.StatusInternalServerError, newErrResp(errors.Wrap(err, "failed to query device")))
		return
	}
	if d != nil && d.Owner != owner.String() {
		slog.Error("failed to check device permission in db", "device_id", req.DeviceID)
		c.JSON(http.StatusForbidden, newErrResp(errors.New("no permission to access the device")))
		return
	}
	if d == nil {
		deviceAddr := common.HexToAddress(strings.TrimPrefix(req.DeviceID, "did:io:"))
		tokenID, err := s.ioidRegistryInstance.DeviceTokenId(nil, deviceAddr)
		if err != nil {
			slog.Error("failed to query device token id", "error", err, "device_id", req.DeviceID)
			c.JSON(http.StatusInternalServerError, newErrResp(errors.Wrap(err, "failed to query device token id")))
			return
		}
		deviceOwner, err := s.ioidInstance.OwnerOf(nil, tokenID)
		if err != nil {
			slog.Error("failed to query device owner", "error", err, "device_id", req.DeviceID, "token_id", tokenID.Uint64())
			c.JSON(http.StatusInternalServerError, newErrResp(errors.Wrap(err, "failed to query device owner")))
			return
		}

		if !bytes.Equal(deviceOwner.Bytes(), owner.Bytes()) {
			slog.Error("failed to check device permission in contract", "device_id", req.DeviceID, "device_owner", deviceOwner.String(), "signature_owner", owner.String())
			c.JSON(http.StatusForbidden, newErrResp(errors.New("no permission to access the device")))
			return
		}

		dev := &db.Device{
			ID:             req.DeviceID,
			Owner:          owner.String(),
			Address:        deviceAddr.String(),
			Status:         db.CONFIRM,
			Proposer:       owner.String(),
			OperationTimes: db.NewOperationTimes(),
		}
		if err := s.db.UpsertDevice(dev); err != nil {
			slog.Error("failed to upsert device", "error", err, "device_id", req.DeviceID)
			c.JSON(http.StatusInternalServerError, newErrResp(errors.Wrap(err, "failed to upsert device")))
			return
		}
		d = dev
	}

	payload, err := base64.RawURLEncoding.DecodeString(req.Payload)
	if err != nil {
		slog.Error("failed to decode base64 data", "error", err)
		c.JSON(http.StatusBadRequest, newErrResp(errors.Wrap(err, "failed to decode base64 data")))
		return
	}
	binPkg, data, err := s.unmarshalPayload(payload)
	if err != nil {
		slog.Error("failed to unmarshal payload", "error", err)
		c.JSON(http.StatusBadRequest, newErrResp(errors.Wrap(err, "failed to unmarshal payload")))
		return
	}
	if err := s.handle(binPkg, data, d); err != nil {
		slog.Error("failed to handle payload data", "error", err)
		c.JSON(http.StatusInternalServerError, newErrResp(errors.Wrap(err, "failed to handle payload data")))
		return
	}
	c.Status(http.StatusOK)
}

func (s *httpServer) unmarshalPayload(payload []byte) (*proto.BinPackage, goproto.Message, error) {
	pkg := &proto.BinPackage{}
	if err := goproto.Unmarshal(payload, pkg); err != nil {
		return nil, nil, errors.Wrap(err, "failed to unmarshal proto")
	}

	var d goproto.Message
	switch t := pkg.GetType(); t {
	case proto.BinPackage_CONFIG:
		d = &proto.SensorConfig{}
	case proto.BinPackage_STATE:
		d = &proto.SensorState{}
	case proto.BinPackage_DATA:
		d = &proto.SensorData{}
	default:
		return nil, nil, errors.Errorf("unexpected senser package type: %d", t)
	}

	err := goproto.Unmarshal(pkg.GetData(), d)
	return pkg, d, errors.Wrapf(err, "failed to unmarshal senser package")
}

func (s *httpServer) handle(binpkg *proto.BinPackage, data goproto.Message, d *db.Device) (err error) {
	switch pkg := data.(type) {
	case *proto.SensorConfig:
		err = s.handleConfig(d, pkg)
	case *proto.SensorState:
		err = s.handleState(d, pkg)
	case *proto.SensorData:
		err = s.handleSensor(binpkg, d, pkg)
	}
	return errors.Wrapf(err, "failed to handle %T", data)
}

func (s *httpServer) handleConfig(dev *db.Device, pkg *proto.SensorConfig) error {
	err := s.db.UpdateByID(dev.ID, map[string]any{
		"bulk_upload":               int32(pkg.GetBulkUpload()),
		"data_channel":              int32(pkg.GetDataChannel()),
		"upload_period":             int32(pkg.GetUploadPeriod()),
		"bulk_upload_sampling_cnt":  int32(pkg.GetBulkUploadSamplingCnt()),
		"bulk_upload_sampling_freq": int32(pkg.GetBulkUploadSamplingFreq()),
		"beep":                      int32(pkg.GetBeep()),
		"real_firmware":             pkg.GetFirmware(),
		"configurable":              pkg.GetDeviceConfigurable(),
		"updated_at":                time.Now(),
	})
	return errors.Wrapf(err, "failed to update device config: %s", dev.ID)
}

func (s *httpServer) handleState(dev *db.Device, pkg *proto.SensorState) error {
	err := s.db.UpdateByID(dev.ID, map[string]any{
		"state":      int32(pkg.GetState()),
		"updated_at": time.Now(),
	})
	return errors.Wrapf(err, "failed to update device state: %s %d", dev.ID, int32(pkg.GetState()))
}

func (s *httpServer) handleSensor(binpkg *proto.BinPackage, dev *db.Device, pkg *proto.SensorData) error {
	snr := float64(pkg.GetSnr())
	if snr > 2700 {
		snr = 100
	} else if snr < 700 {
		snr = 25
	} else {
		snr, _ = big.NewFloat((snr-700)*0.0375 + 25).Float64()
	}

	vbat := (float64(pkg.GetVbat()) - 320) / 90
	if vbat > 1 {
		vbat = 100
	} else if vbat < 0.1 {
		vbat = 0.1
	} else {
		vbat *= 100
	}

	gyroscope, _ := json.Marshal(pkg.GetGyroscope())
	accelerometer, _ := json.Marshal(pkg.GetAccelerometer())

	dr := &db.DeviceRecord{
		ID:             dev.ID + "-" + fmt.Sprintf("%d", binpkg.GetTimestamp()),
		Imei:           dev.ID,
		Timestamp:      int64(binpkg.GetTimestamp()),
		Signature:      hex.EncodeToString(append(binpkg.GetSignature(), 0)),
		Operator:       "",
		Snr:            strconv.FormatFloat(snr, 'f', 1, 64),
		Vbat:           strconv.FormatFloat(vbat, 'f', 1, 64),
		Latitude:       decimal.NewFromInt32(pkg.GetLatitude()).Div(decimal.NewFromInt32(10000000)).StringFixed(7),
		Longitude:      decimal.NewFromInt32(pkg.GetLongitude()).Div(decimal.NewFromInt32(10000000)).StringFixed(7),
		GasResistance:  decimal.NewFromInt32(int32(pkg.GetGasResistance())).Div(decimal.NewFromInt32(100)).StringFixed(2),
		Temperature:    decimal.NewFromInt32(pkg.GetTemperature()).Div(decimal.NewFromInt32(100)).StringFixed(2),
		Temperature2:   decimal.NewFromInt32(int32(pkg.GetTemperature2())).Div(decimal.NewFromInt32(100)).StringFixed(2),
		Pressure:       decimal.NewFromInt32(int32(pkg.GetPressure())).Div(decimal.NewFromInt32(100)).StringFixed(2),
		Humidity:       decimal.NewFromInt32(int32(pkg.GetHumidity())).Div(decimal.NewFromInt32(100)).StringFixed(2),
		Light:          decimal.NewFromInt32(int32(pkg.GetLight())).Div(decimal.NewFromInt32(100)).StringFixed(2),
		Gyroscope:      string(gyroscope),
		Accelerometer:  string(accelerometer),
		OperationTimes: db.NewOperationTimes(),
	}
	err := s.db.CreateDeviceRecord(dr)
	return errors.Wrapf(err, "failed to create senser data: %s", dev.ID)
}

func Run(db *db.DB, address string, client *ethclient.Client, ioidAddr, ioidRegistryAddr common.Address) error {
	ioidInstance, err := ioid.NewIoid(ioidAddr, client)
	if err != nil {
		return errors.Wrap(err, "failed to new ioid contract instance")
	}
	ioidRegistryInstance, err := ioidregistry.NewIoidregistry(ioidRegistryAddr, client)
	if err != nil {
		return errors.Wrap(err, "failed to new ioid registry contract instance")
	}
	s := &httpServer{
		engine:               gin.Default(),
		db:                   db,
		ioidInstance:         ioidInstance,
		ioidRegistryInstance: ioidRegistryInstance,
	}

	s.engine.GET("/device", s.query)
	s.engine.POST("/device", s.receive)

	err = s.engine.Run(address)
	return errors.Wrap(err, "failed to start http server")
}
