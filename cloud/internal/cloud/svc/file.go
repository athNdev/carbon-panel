package svc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/google/uuid"
)

// FileService implements FileServiceHandler for remote file operations on workloads.
type FileService struct {
	deps       Deps
	dispatcher *AgentDispatcher
}

func (s *FileService) load(ctx context.Context, id string) (*db.Workload, error) {
	q, err := s.deps.Store.Org(ctx)
	if err != nil {
		return nil, err
	}
	var w db.Workload
	if err := q.Where("id = ?", id).First(&w).Error; err != nil {
		return nil, errWorkloadNotFound
	}
	return &w, nil
}

func (s *FileService) checkWorkloadNode(w *db.Workload) error {
	if w.NodeID == "" {
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("workload is not placed on any node"))
	}
	if s.dispatcher == nil || !s.dispatcher.IsConnected(w.NodeID) {
		return connect.NewError(connect.CodeUnavailable, errors.New("node agent is offline"))
	}
	return nil
}

// ListFiles lists directory contents for a workload.
func (s *FileService) ListFiles(ctx context.Context, req *connect.Request[v1.ListFilesRequest]) (*connect.Response[v1.ListFilesResponse], error) {
	if req.Msg.WorkloadId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id is required"))
	}
	w, err := s.load(ctx, req.Msg.WorkloadId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if err := s.checkWorkloadNode(w); err != nil {
		return nil, err
	}

	commandID := uuid.NewString()
	waitCh := s.dispatcher.ExpectFileList(commandID)
	defer s.dispatcher.CancelFileList(commandID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_FileList{
			FileList: &v1.ControlFileList{
				CommandId:  commandID,
				WorkloadId: w.ID,
				Path:       req.Msg.Path,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch file list to node agent"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(15 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for file list from node agent"))
	case res := <-waitCh:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("list files failed: %s", res.Error))
		}
		return connect.NewResponse(&v1.ListFilesResponse{
			Files: res.Files,
		}), nil
	}
}

// StatFile returns metadata for a file or directory.
func (s *FileService) StatFile(ctx context.Context, req *connect.Request[v1.StatFileRequest]) (*connect.Response[v1.StatFileResponse], error) {
	if req.Msg.WorkloadId == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id is required"))
	}
	w, err := s.load(ctx, req.Msg.WorkloadId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if err := s.checkWorkloadNode(w); err != nil {
		return nil, err
	}

	commandID := uuid.NewString()
	waitCh := s.dispatcher.ExpectFileStat(commandID)
	defer s.dispatcher.CancelFileStat(commandID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_FileStat{
			FileStat: &v1.ControlStatFile{
				CommandId:  commandID,
				WorkloadId: w.ID,
				Path:       req.Msg.Path,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch stat to node agent"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(15 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for file stat from node agent"))
	case res := <-waitCh:
		if !res.Success {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("stat failed: %s", res.Error))
		}
		return connect.NewResponse(&v1.StatFileResponse{
			Info: res.Info,
		}), nil
	}
}

// ReadFile streams file chunks (bounded to 64KB) from a workload.
func (s *FileService) ReadFile(ctx context.Context, req *connect.Request[v1.ReadFileRequest], stream *connect.ServerStream[v1.ReadFileResponse]) error {
	if req.Msg.WorkloadId == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id is required"))
	}
	w, err := s.load(ctx, req.Msg.WorkloadId)
	if err != nil {
		return connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if err := s.checkWorkloadNode(w); err != nil {
		return err
	}

	commandID := uuid.NewString()
	chunkCh := s.dispatcher.ExpectFileChunk(commandID)
	defer s.dispatcher.CancelFileChunk(commandID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_FileRead{
			FileRead: &v1.ControlReadFile{
				CommandId:  commandID,
				WorkloadId: w.ID,
				Path:       req.Msg.Path,
			},
		},
	})
	if !ok {
		return connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch file read to node agent"))
	}

	for {
		select {
		case <-ctx.Done():
			return connect.NewError(connect.CodeCanceled, ctx.Err())
		case <-time.After(30 * time.Second):
			return connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for file chunks from node agent"))
		case chunk, ok := <-chunkCh:
			if !ok {
				return nil
			}
			if !chunk.Success {
				return connect.NewError(connect.CodeInternal, fmt.Errorf("read file failed: %s", chunk.Error))
			}
			if err := stream.Send(&v1.ReadFileResponse{
				Chunk:     chunk.Chunk,
				IsLast:    chunk.IsLast,
				TotalSize: chunk.TotalSize,
			}); err != nil {
				return err
			}
			if chunk.IsLast {
				return nil
			}
		}
	}
}

// WriteFile writes or streams file chunks (bounded to 64KB) to a workload.
func (s *FileService) WriteFile(ctx context.Context, stream *connect.ClientStream[v1.WriteFileRequest]) (*connect.Response[v1.WriteFileResponse], error) {
	if !stream.Receive() {
		if err := stream.Err(); err != nil {
			return nil, err
		}
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("empty write stream"))
	}

	first := stream.Msg()
	if strings.TrimSpace(first.WorkloadId) == "" || strings.TrimSpace(first.Path) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id and path are required"))
	}

	w, err := s.load(ctx, first.WorkloadId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if err := s.checkWorkloadNode(w); err != nil {
		return nil, err
	}

	commandID := uuid.NewString()
	waitCh := s.dispatcher.ExpectFileWrite(commandID)
	defer s.dispatcher.CancelFileWrite(commandID)

	// Send first chunk
	ok := s.dispatcher.DispatchContext(ctx, w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_FileWrite{
			FileWrite: &v1.ControlWriteFileChunk{
				CommandId:  commandID,
				WorkloadId: w.ID,
				Path:       first.Path,
				Chunk:      first.Chunk,
				IsLast:     first.IsLast,
				Mode:       first.Mode,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch write chunk to node agent"))
	}

	// If there are subsequent chunks in client stream
	if !first.IsLast {
		for stream.Receive() {
			chunkMsg := stream.Msg()
			ok := s.dispatcher.DispatchContext(ctx, w.NodeID, &v1.ControlMessage{
				Payload: &v1.ControlMessage_FileWrite{
					FileWrite: &v1.ControlWriteFileChunk{
						CommandId:  commandID,
						WorkloadId: w.ID,
						Path:       first.Path,
						Chunk:      chunkMsg.Chunk,
						IsLast:     chunkMsg.IsLast,
						Mode:       first.Mode,
					},
				},
			})
			if !ok {
				return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch subsequent write chunk to node agent"))
			}
			if chunkMsg.IsLast {
				break
			}
		}
		if err := stream.Err(); err != nil {
			return nil, err
		}
	}

	// Await completion result
	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(30 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for write confirmation from node agent"))
	case res := <-waitCh:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("write failed: %s", res.Error))
		}
		return connect.NewResponse(&v1.WriteFileResponse{
			BytesWritten: res.BytesWritten,
			Path:         first.Path,
		}), nil
	}
}

// DeleteFile removes a file or directory from a workload.
func (s *FileService) DeleteFile(ctx context.Context, req *connect.Request[v1.DeleteFileRequest]) (*connect.Response[v1.DeleteFileResponse], error) {
	if req.Msg.WorkloadId == "" || req.Msg.Path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id and path are required"))
	}
	w, err := s.load(ctx, req.Msg.WorkloadId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if err := s.checkWorkloadNode(w); err != nil {
		return nil, err
	}

	commandID := uuid.NewString()
	waitCh := s.dispatcher.ExpectFileDelete(commandID)
	defer s.dispatcher.CancelFileDelete(commandID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_FileDelete{
			FileDelete: &v1.ControlDeleteFile{
				CommandId:  commandID,
				WorkloadId: w.ID,
				Path:       req.Msg.Path,
				Recursive:  req.Msg.Recursive,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch delete to node agent"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(15 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for delete from node agent"))
	case res := <-waitCh:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete failed: %s", res.Error))
		}
		return connect.NewResponse(&v1.DeleteFileResponse{}), nil
	}
}

// CreateDirectory creates a directory (and parents) in a workload.
func (s *FileService) CreateDirectory(ctx context.Context, req *connect.Request[v1.CreateDirectoryRequest]) (*connect.Response[v1.CreateDirectoryResponse], error) {
	if req.Msg.WorkloadId == "" || req.Msg.Path == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id and path are required"))
	}
	w, err := s.load(ctx, req.Msg.WorkloadId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if err := s.checkWorkloadNode(w); err != nil {
		return nil, err
	}

	commandID := uuid.NewString()
	waitCh := s.dispatcher.ExpectDirCreate(commandID)
	defer s.dispatcher.CancelDirCreate(commandID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_DirCreate{
			DirCreate: &v1.ControlCreateDirectory{
				CommandId:  commandID,
				WorkloadId: w.ID,
				Path:       req.Msg.Path,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch create directory to node agent"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(15 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for create directory from node agent"))
	case res := <-waitCh:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create directory failed: %s", res.Error))
		}
		return connect.NewResponse(&v1.CreateDirectoryResponse{}), nil
	}
}

// RenameFile renames or moves a file or directory within a workload.
func (s *FileService) RenameFile(ctx context.Context, req *connect.Request[v1.RenameFileRequest]) (*connect.Response[v1.RenameFileResponse], error) {
	if req.Msg.WorkloadId == "" || req.Msg.OldPath == "" || req.Msg.NewPath == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload_id, old_path, and new_path are required"))
	}
	w, err := s.load(ctx, req.Msg.WorkloadId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errWorkloadNotFound)
	}
	if err := s.checkWorkloadNode(w); err != nil {
		return nil, err
	}

	commandID := uuid.NewString()
	waitCh := s.dispatcher.ExpectFileRename(commandID)
	defer s.dispatcher.CancelFileRename(commandID)

	ok := s.dispatcher.Dispatch(w.NodeID, &v1.ControlMessage{
		Payload: &v1.ControlMessage_FileRename{
			FileRename: &v1.ControlFileRename{
				CommandId:  commandID,
				WorkloadId: w.ID,
				OldPath:    req.Msg.OldPath,
				NewPath:    req.Msg.NewPath,
			},
		},
	})
	if !ok {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("failed to dispatch rename to node agent"))
	}

	select {
	case <-ctx.Done():
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case <-time.After(15 * time.Second):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, errors.New("timed out waiting for rename from node agent"))
	case res := <-waitCh:
		if !res.Success {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("rename failed: %s", res.Error))
		}
		return connect.NewResponse(&v1.RenameFileResponse{}), nil
	}
}
