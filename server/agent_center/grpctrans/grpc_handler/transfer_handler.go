package grpc_handler

import (
	"context"
	"errors"
	"time"

	"github.com/bytedance/Elkeid/server/agent_center/common"
	"github.com/bytedance/Elkeid/server/agent_center/common/ylog"
	"github.com/bytedance/Elkeid/server/agent_center/grpctrans/pool"
	pb "github.com/bytedance/Elkeid/server/agent_center/grpctrans/proto"
	"google.golang.org/grpc/peer"
)

// 指令 && 数据的 grpc stream实现

/*
service Transfer {
  rpc Transfer (stream RawData) returns (stream Command){}
}
*/

type TransferHandler struct{}

var (
	//GlobalGRPCPool is the global grpc connection manager
	GlobalGRPCPool *pool.GRPCPool
)

func InitGlobalGRPCPool() {
	option := pool.NewConfig()
	option.PoolLength = common.ConnLimit
	GlobalGRPCPool = pool.NewGRPCPool(option)
}

// rpc method（长连接）
func (h *TransferHandler) Transfer(stream pb.Transfer_TransferServer) error {
	//Maximum number of concurrent connections

	// 并发控制
	if !GlobalGRPCPool.LoadToken() {
		err := errors.New("out of max connection limit")
		ylog.Errorf("Transfer", err.Error())
		return err
	}
	defer func() {
		GlobalGRPCPool.ReleaseToken()
	}()

	//Receive the first packet and get the AgentID

	// 从stream获取agent发送的第一个报文
	data, err := stream.Recv()
	if err != nil {
		ylog.Errorf("Transfer", "Transfer error %s", err.Error())
		return err
	}
	agentID := data.AgentID

	//Get the client address
	p, ok := peer.FromContext(stream.Context())
	if !ok {
		ylog.Errorf("Transfer", "Transfer error %s", err.Error())
		return err
	}
	addr := p.Addr.String()
	ylog.Infof("Transfer", ">>>>connection addr: %s", addr)

	//add connection info to the GlobalGRPCPool
	ctx, cancelButton := context.WithCancel(context.Background())
	createAt := time.Now().UnixNano() / (1000 * 1000 * 1000)
	connection := pool.Connection{
		AgentID:     agentID,
		SourceAddr:  addr,
		CreateAt:    createAt,
		CommandChan: make(chan *pool.Command),
		Ctx:         ctx,
		CancelFuc:   cancelButton,
	}
	ylog.Infof("Transfer", ">>>>now set %s %v", agentID, connection)

	// 向GlobalGRPCPool（在线连接池）保存此agent的连接信息
	err = GlobalGRPCPool.Add(agentID, &connection)
	if err != nil {
		ylog.Errorf("Transfer", "Transfer error %s", err.Error())
		return err
	}
	defer func() {
		ylog.Infof("Transfer", "now delete %s ", agentID)
		GlobalGRPCPool.Delete(agentID)
		releaseAgentHeartbeatMetrics(agentID)
	}()

	//Process the first of data
	handleRawData(data, &connection)

	//Receive data from agent（接收agent的数据stream.Recv()）
	go recvData(stream, &connection)
	//Send command to agent（向agent发送数据 stream.Send()）
	go sendData(stream, &connection)

	// stream是一个长连接，这里等待上层主动结束
	//stop here
	<-connection.Ctx.Done()
	return nil
}

func recvData(stream pb.Transfer_TransferServer, conn *pool.Connection) {
	defer conn.CancelFuc()

	for {
		select {
		case <-conn.Ctx.Done():
			ylog.Errorf("recvData", "the send direction of the tcp is closed, now close the recv direction, %s ", conn.AgentID)
			return
		default:
			data, err := stream.Recv()
			if err != nil {
				ylog.Errorf("recvData", "Transfer Recv Error %s, now close the recv direction of the tcp, %s ", err.Error(), conn.AgentID)
				return
			}
			recvCounter.Inc()
			handleRawData(data, conn)
		}
	}
}

func sendData(stream pb.Transfer_TransferServer, conn *pool.Connection) {
	defer conn.CancelFuc()

	for {
		select {
		case <-conn.Ctx.Done():
			ylog.Infof("sendData", "the recv direction of the tcp is closed, now close the send direction, %s ", conn.AgentID)
			return
		case cmd := <-conn.CommandChan: //SEND是被动触发的，由API主动写入指令到conn.CommandChan中
			//if cmd is nil, close the connection
			if cmd == nil {
				ylog.Infof("sendData", "get the close signal , now close the send direction of the tcp, %s ", conn.AgentID)
				return
			}
			err := stream.Send(cmd.Command)
			if err != nil {
				ylog.Errorf("sendData", "Send Task Error %s %v ", conn.AgentID, cmd)
				cmd.Error = err
				close(cmd.Ready)
				return
			}
			sendCounter.Inc()
			ylog.Infof("sendData", "Transfer Send %s %v ", conn.AgentID, cmd)
			cmd.Error = nil
			close(cmd.Ready)
		}
	}
}
