package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JFJun/substrate-go/config"
	v11 "github.com/JFJun/substrate-go/model/v11"
	"github.com/JFJun/substrate-go/scale"
	"github.com/JFJun/substrate-go/ss58"
	"github.com/JFJun/substrate-go/state"
	"github.com/JFJun/substrate-go/util"
	codes "github.com/itering/scale.go"
	"github.com/itering/scale.go/source"
	"github.com/itering/scale.go/types"
	"github.com/itering/scale.go/utiles"
	"golang.org/x/crypto/blake2b"
	"log"
	"math/big"
	"strconv"
	"strings"
)

type Client struct {
	Rpc                IClient
	Metadata           *codes.MetadataDecoder
	CoinType           string
	SpecVersion        int
	TransactionVersion int
	genesisHash        string
}
type CringAccountInfo struct {
	Nonce    state.U32 `json:"nonce"`
	Refcount state.U32 `json:"ref_count"`
	Providers state.U32 `json:"providers"`
	Sufficients state.U32 `json:"sufficients"`
	Data     struct {
		Free       state.U128 `json:"free"`
		Reserved   state.U128 `json:"reserved"`
		MiscFrozen state.U128 `json:"misc_frozen"`
		FreeFrozen state.U128 `json:"free_frozen"`
	} `json:"data"`
}
type IClient interface {
	SendRequest(method string, params []interface{}) ([]byte, error)
}

func New(url, user, password string) (*Client, error) {
	client := new(Client)
	if strings.HasPrefix(url, "wss") || strings.HasPrefix(url, "ws") {
		socket := util.NewWebsocket(url)
		client.Rpc = &socket
	} else if strings.HasPrefix(url, "http") || strings.HasPrefix(url, "https") {
		client.Rpc = util.New(url, user, password)
	} else {
		return nil, fmt.Errorf("unsopport url=%s", url)
	}
	//初始化运行版本
	err := client.initRuntimeVersion()
	if err != nil {
		return nil, err
	}
	client.registerTypes()
	return client, nil
}

func (client *Client) initMetaData() error {
	metadataBytes, err := client.Rpc.SendRequest("state_getMetadata", []interface{}{})
	if err != nil {
		return fmt.Errorf("rpc get metadata error,err=%v", err)
	}
	metadata := string(metadataBytes)
	metadata = util.RemoveHex0x(metadata)
	data, err := hex.DecodeString(metadata)
	if err != nil {
		return err
	}
	m := codes.MetadataDecoder{}
	m.Init(data)
	if err := m.Process(); err != nil {
		return fmt.Errorf("parse metadata error,err=%v", err)
	}
	client.Metadata = &m
	return nil
}

/*
注册types
*/
func (client *Client) registerTypes() {
	ccHex := config.CoinEventType[client.CoinType]
	cc, _ := hex.DecodeString(ccHex)
	types.RegCustomTypes(source.LoadTypeRegistry(cc))
}
func (client *Client) initRuntimeVersion() error {

	data, err := client.Rpc.SendRequest("state_getRuntimeVersion", []interface{}{})
	if err != nil {
		return fmt.Errorf("init runtime version error,err=%v", err)
	}
	var result map[string]interface{}
	errJ := json.Unmarshal(data, &result)
	if errJ != nil {
		return fmt.Errorf("init runtime version error,err=%v", errJ)
	}
	client.CoinType = strings.ToLower(result["specName"].(string))
	client.TransactionVersion = int(result["transactionVersion"].(float64))
	specVersion := int(result["specVersion"].(float64))
	// metadata 会动态改变，所以通过specVersion去检测metadata的改变
	if client.SpecVersion != specVersion {
		client.SpecVersion = specVersion
		return client.initMetaData()
	}
	client.SpecVersion = specVersion
	return nil
}

func (client *Client) GetBlockNumber(blockHash string) (int64, error) {
	var (
		resp []byte
		err  error
	)
	if blockHash == "" {
		blockHash, err = client.GetFinalizedHead()
		if err != nil {
			return -1, err
		}
	}
	resp, err = client.Rpc.SendRequest("chain_getBlock", []interface{}{blockHash})
	if err != nil {
		return -1, err
	}
	var block v11.SignedBlock
	err = json.Unmarshal(resp, &block)
	if err != nil {
		return -1, err
	}

	b, isOK := new(big.Int).SetString(block.Block.Header.Number[2:], 16)
	if !isOK {
		return -1, errors.New("parse hex block number error")
	}
	return b.Int64(), nil
}

func (client *Client) GetFinalizedHead() (string, error) {
	resp, err := client.Rpc.SendRequest("chain_getFinalizedHead", []interface{}{})
	if err != nil {
		return "", err
	}
	return string(resp), nil
}

func (client *Client) GetGenesisHash() string {
	if client.genesisHash != "" {
		return client.genesisHash
	}
	resp, err := client.Rpc.SendRequest("chain_getBlockHash", []interface{}{0})
	if err != nil {
		return ""
	}
	client.genesisHash = string(resp)
	return string(resp)
}

func (client *Client) GetAccountInfo(address string) ([]byte, error) {
	errV := client.initRuntimeVersion()
	if errV != nil {
		return nil, errV
	}
	pub, err := ss58.DecodeToPub(address)
	if err != nil {
		return nil, err
	}
	key, err1 := state.CreateStorageKey(client.Metadata, "System", "Account", pub, nil)
	if err1 != nil {
		return nil, fmt.Errorf("create stroage key error,err=%v", err1)
	}

	resp, err2 := client.Rpc.SendRequest("state_getStorageAt", []interface{}{key})
	if err2 != nil {
		return nil, err2
	}
	respStr := util.RemoveHex0x(string(resp))
	data, _ := hex.DecodeString(respStr)
	raw := state.NewStorageDataRaw(data)
	var target state.AccountInfo
	if strings.ToLower(client.CoinType) == "crab"{
		tmpgarget := new(CringAccountInfo)
		scale.NewDecoder(bytes.NewReader(raw)).Decode(tmpgarget)
		if &target == nil {
			return nil, fmt.Errorf("decode stroage data error,data=[%s]", data)
		}
		target.Nonce = tmpgarget.Nonce
		target.Refcount = tmpgarget.Refcount
		target.Data.Free = tmpgarget.Data.Free
		target.Data.Reserved = tmpgarget.Data.Reserved
		target.Data.MiscFrozen = tmpgarget.Data.MiscFrozen
		target.Data.FreeFrozen = tmpgarget.Data.FreeFrozen
		return json.Marshal(target)
	}
	scale.NewDecoder(bytes.NewReader(raw)).Decode(&target)
	if &target == nil {
		return nil, fmt.Errorf("decode stroage data error,data=[%s]", data)
	}
	return json.Marshal(target)
}

/*
根据高度获取对应的区块信息以及交易信息
*/
func (client *Client) GetBlockByNumber(height int64) (*v11.BlockResponse, error) {
	var (
		respData []byte
		err      error
	)
	respData, err = client.Rpc.SendRequest("chain_getBlockHash", []interface{}{height})
	if err != nil || len(respData) == 0 {
		return nil, fmt.Errorf("get block hash error,err=%v", err)
	}
	blockHash := string(respData)
	return client.GetBlockByHash(blockHash)
}

func (client *Client) GetBlockByHash(blockHash string) (*v11.BlockResponse, error) {
	var (
		respData []byte
		err      error
	)
	errV := client.initRuntimeVersion()
	if errV != nil {
		return nil, errV
	}
	respData, err = client.Rpc.SendRequest("chain_getBlock", []interface{}{blockHash})
	if err != nil || len(respData) == 0 {
		return nil, fmt.Errorf("get block error,err=%v", err)
	}
	var block v11.SignedBlock
	err = json.Unmarshal(respData, &block)
	if err != nil {
		return nil, fmt.Errorf("parse block error")
	}
	blockResp := new(v11.BlockResponse)
	number, _ := strconv.ParseInt(util.RemoveHex0x(block.Block.Header.Number), 16, 64)
	blockResp.Height = number
	blockResp.ParentHash = block.Block.Header.ParentHash
	blockResp.BlockHash = blockHash
	if len(block.Block.Extrinsics) > 0 {
		//extrinsicNum:=len(block.Block.Extrinsics)
		err = client.parseExtrinsicByDecode(block.Block.Extrinsics, blockResp)
		if err != nil {
			return nil, err
		}
		err = client.parseExtrinsicByStorage(blockHash, blockResp)
		if err != nil {
			return nil, err
		}
	}

	return blockResp, nil
}

type parseBlockExtrinsicParams struct {
	from, to, sig, era, txid string
	nonce                    int64
	extrinsicIdx             int
}

func (client *Client) parseExtrinsicByDecode(extrinsics []string, blockResp *v11.BlockResponse) error {

	var (
		params    []parseBlockExtrinsicParams
		timestamp int64
		//idx int
	)
	defer func() {
		if err := recover(); err != nil {
			blockResp.Timestamp = timestamp
			blockResp.Extrinsic = []*v11.ExtrinsicResponse{}
			log.Printf("parse %d block extrinsic error, err=%v\n", blockResp.Height, err)
		}
	}()

	for i, extrinsic := range extrinsics {
		//idx = i
		e := codes.ExtrinsicDecoder{}
		option := types.ScaleDecoderOption{Metadata: &client.Metadata.Metadata}
		e.Init(types.ScaleBytes{Data: utiles.HexToBytes(extrinsic)}, &option)
		e.Process()
		bb, err := json.Marshal(e.Value)
		if err != nil {
			return fmt.Errorf("parse extrinsic error,err=%v", err)
		}
		var resp v11.ExtrinsicDecodeResponse
		errM := json.Unmarshal(bb, &resp)
		if errM != nil {
			return fmt.Errorf("json unmarshal extrinsic error,err=%v", errM)
		}
		switch resp.CallModule {
		case "Timestamp":
			for _, param := range resp.Params {
				if param.Name == "now" {
					timestamp = int64(param.Value.(float64))
				}
			}
		case "Balances":
			if resp.CallModuleFunction == "transfer" || resp.CallModuleFunction == "transfer_keep_alive" {
				blockData := parseBlockExtrinsicParams{}
				blockData.from, _ = ss58.EncodeByPubHex(resp.AccountId, config.PrefixMap[client.CoinType])
				blockData.era = resp.Era
				blockData.sig = resp.Signature
				blockData.nonce = resp.Nonce
				blockData.extrinsicIdx = i
				blockData.txid = createTxHash(extrinsic)
				for _, param := range resp.Params {
					if param.Name == "dest" {
						blockData.to, _ = ss58.EncodeByPubHex(param.Value.(string), config.PrefixMap[client.CoinType])
					}
				}
				params = append(params, blockData)
			}
		case "Utility":
			if resp.CallModuleFunction == "batch" {
				for _, param := range resp.Params {
					if param.Name == "calls" {
						switch param.Value.(type) {
						case []interface{}:
							d, _ := json.Marshal(param.Value)
							var values []v11.UtilityParamsValue
							err = json.Unmarshal(d, &values)
							if err != nil {
								continue
							}
							for _, value := range values {
								if value.CallModule == "Balances" {
									if value.CallFunction == "transfer" || value.CallFunction == "transfer_keep_alive" {
										if len(value.CallArgs) > 0 {
											for _, arg := range value.CallArgs {
												if arg.Name == "dest" {
													blockData := parseBlockExtrinsicParams{}
													blockData.from, _ = ss58.EncodeByPubHex(resp.AccountId, config.PrefixMap[client.CoinType])
													blockData.era = resp.Era
													blockData.sig = resp.Signature
													blockData.nonce = resp.Nonce
													blockData.extrinsicIdx = i
													blockData.txid = createTxHash(extrinsic)
													blockData.to, _ = ss58.EncodeByPubHex(arg.ValueRaw, config.PrefixMap[client.CoinType])
													params = append(params, blockData)
												}
											}
										}
									}
								}
							}
						default:
							continue
						}
					}
				}
			}

		//case "Claims": //crab 转账call_module
		//	blockData := parseBlockExtrinsicParams{}
		//	blockData.from, _ = ss58.EncodeByPubHex(resp.AccountId, config.PrefixMap[client.CoinType])
		//	blockData.era = resp.Era
		//	blockData.sig = resp.Signature
		//	blockData.nonce = resp.Nonce
		//	blockData.extrinsicIdx = i
		//	blockData.txid = createTxHash(extrinsic)
		//	for _, param := range resp.Params {
		//		if param.Name == "dest" {
		//			blockData.to, _ = ss58.EncodeByPubHex(param.ValueRaw, config.PrefixMap[client.CoinType])
		//		}
		//	}
		//	params = append(params, blockData)
		default:
			//todo  add another call_module 币种不同可能使用的call_module不一样
			continue
		}

	}
	blockResp.Timestamp = timestamp
	//解析params
	if len(params) == 0 {

		blockResp.Extrinsic = []*v11.ExtrinsicResponse{}
		return nil
	}
	blockResp.Extrinsic = make([]*v11.ExtrinsicResponse, len(params))
	for idx, param := range params {
		// write by jun 2020/06/18
		// 避免不同高度出现相同txid的情况  详情高度： 552851  552911
		//txid := fmt.Sprintf("%s_%d-%d", param.txid, blockResp.Height, param.extrinsicIdx)
		e := new(v11.ExtrinsicResponse)
		e.Signature = param.sig
		e.FromAddress = param.from
		e.ToAddress = param.to
		e.Nonce = param.nonce
		e.Era = param.era
		e.ExtrinsicIndex = param.extrinsicIdx
		//e.Txid = txid
		e.Txid = param.txid
		blockResp.Extrinsic[idx] = e
	}

	return nil
}

func (client *Client) parseExtrinsicByStorage(blockHash string, blockResp *v11.BlockResponse) error {
	var (
		err  error
		key  string
		resp []byte
	)
	defer func() {
		if err := recover(); err != nil {
			log.Printf("parse %d block event error,Err=[%v]", blockResp.Height, err)
		}
	}()
	key, err = state.CreateStorageKey(client.Metadata, "System", "Events", nil, nil)
	if err != nil {
		return fmt.Errorf("create stroage key error,err=%v", err)
	}
	resp, err = client.Rpc.SendRequest("state_getStorage", []interface{}{key, blockHash})
	if err != nil || len(resp) <= 0 {
		return fmt.Errorf("get system events error,err=%v", err)
	}
	eventsHex := string(resp)
	//解析events
	option := types.ScaleDecoderOption{Metadata: &client.Metadata.Metadata, Spec: client.SpecVersion}
	e := codes.EventsDecoder{}
	e.Init(types.ScaleBytes{Data: utiles.HexToBytes(eventsHex)}, &option)
	e.Process()
	data, err1 := json.Marshal(e.Value)
	if err1 != nil {
		return err
	}
	var eventResp []v11.EventResponse
	err = json.Unmarshal(data, &eventResp)
	if err != nil {
		return fmt.Errorf("parse events error,err=%v", err)
	}

	//使用新的处理event方法
	if len(eventResp) > 0 {
		statusMap := make(map[int]bool)
		var result []v11.EventResult
		for _, event := range eventResp {
			switch event.EventId {
			case config.ExtrinsicFailed:
				//如果是失败，记录下来是哪一笔交易失败
				statusMap[event.ExtrinsicIdx] = false
			case config.ExtrinsicSuccess:
				statusMap[event.ExtrinsicIdx] = true
			case config.Transfer:
				if event.ModuleId == "Balances" {
					if len(event.Params) <= 0 {
						statusMap[event.ExtrinsicIdx] = false
						continue
					}
					var res v11.EventResult
					res.ExtrinsicIdx = event.ExtrinsicIdx
					res.EventIdx = event.EventIdx
					for i, param := range event.Params {
						if param.Type == "AccountId" {
							if i == 0 {
								from, _ := ss58.EncodeByPubHex(param.Value.(string), config.PrefixMap[client.CoinType])
								res.From = from
							}
							if i == 1 {
								to, _ := ss58.EncodeByPubHex(param.Value.(string), config.PrefixMap[client.CoinType])
								res.To = to
							}
						}
						if param.Type == "Balance" {
							res.Amount = param.Value.(string)
						}
					}
					result = append(result, res)
				}
			default:
				continue
			}
		}
		for _, e := range blockResp.Extrinsic {

			for _, res := range result {
				if e.ExtrinsicIndex == res.ExtrinsicIdx && e.ToAddress == res.To {
					//判断是否是有效交易
					if statusMap[e.ExtrinsicIndex] {
						e.Status = "success"
					} else {
						e.Status = "fail"
					}
					e.Type = "transfer"
					e.Amount = res.Amount
					e.Fee = client.calcFee(eventResp, e.ExtrinsicIndex)
					e.EventIndex = res.EventIdx
					e.ToAddress = res.To //extrinsic 里面的to地址有可能解析错误，但是event里面的to地址是正确的，所以使用event里面的他to地址
				}
			}
		}
	}
	return nil
	//-----------------------old--------------------//
	//if len(eventResp) > 0 {
	//  for _, event := range eventResp {
	//    var (
	//      defaultSuccess = "success"
	//      amount         = "0"
	//    )
	//    switch event.EventId {
	//    case config.ExtrinsicFailed:
	//      defaultSuccess = "failed"
	//      break
	//    case config.Transfer:
	//      if event.ModuleId == "Balances" {
	//        if len(event.Params) <= 0 {
	//          defaultSuccess = "failed"
	//          continue
	//        }
	//        for _, param := range event.Params {
	//          if param.Type == "Balance" {
	//            amount = param.Value.(string)
	//          }
	//        }
	//      }
	//    default:
	//      continue
	//    }
	//
	//    for _, e := range blockResp.Extrinsic {
	//      if e.ExtrinsicIndex == event.ExtrinsicIdx {
	//        e.Type = "transfer"
	//        e.Amount = amount
	//        e.Status = defaultSuccess
	//        e.Fee = client.calcFee(eventResp, event.ExtrinsicIdx)
	//      }
	//    }
	//
	//  }
	//  ////设置交易状态
	//  //blockResp.Status = defaultSuccess
	//  //if defaultSuccess=="failed" {
	//  //  return nil
	//  //}
	//  ////在做一次for循环计算手续费
	//  //blockResp.Extrinsic.Fee=client.calcFee(eventResp,extrinsicIdx)
	//}
	//return error
}

/*
todo maybe have anther fee events
*/
func (client *Client) calcFee(events []v11.EventResponse, extrinsicIdx int) string {
	fee := new(big.Int).SetInt64(0)
	for _, event := range events {
		if event.ExtrinsicIdx == extrinsicIdx {
			if config.IsContainFeeEventId(event.EventId) {
				switch event.ModuleId {
				case "Treasury":
					if len(event.Params) == 0 {
						continue
					}
					for _, param := range event.Params {
						if strings.Contains(param.Type, "Balance") {
							value := param.Value.(string)
							subFee, isOk := new(big.Int).SetString(value, 10)
							if !isOk {
								continue
							}
							fee = fee.Add(fee, subFee)
						}
					}
				case "Balances":
					if len(event.Params) == 0 {
						continue
					}
					for _, param := range event.Params {
						if strings.Contains(param.Type, "Balance") {
							value := param.Value.(string)
							subFee, isOk := new(big.Int).SetString(value, 10)
							if !isOk {
								continue
							}
							fee = fee.Add(fee, subFee)
						}
					}

				default:
					continue
				}
			}
		}
	}
	return fee.String()
}

func createTxHash(extrinsic string) string {
	data, _ := hex.DecodeString(util.RemoveHex0x(extrinsic))
	d := blake2b.Sum256(data)
	return "0x" + hex.EncodeToString(d[:])
}

/*
使用的第三方包没有提供查找callidx的接口，所以得自己遍历查找，没的办法
*/
func (client *Client) GetCallIdx(moduleName, fn string) (callIdx string, err error) {
	//避免指针为空
	defer func() {
		if errs := recover(); errs != nil {
			callIdx = ""
			err = fmt.Errorf("catch panic ,err=%v", errs)
		}
	}()

	for _, mod := range client.Metadata.Metadata.Metadata.Modules {
		if mod.Name == moduleName {
			for _, call := range mod.Calls {
				if call.Name == fn {
					return call.Lookup, nil
				}
			}
		}
	}
	return "", errors.New("do not find this call index")
}
