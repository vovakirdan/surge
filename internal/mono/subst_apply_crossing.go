package mono

import "surge/internal/hir"

func (s *Subst) applyCrossingData(data *hir.CrossingData) error {
	if s == nil || data == nil {
		return nil
	}
	data.Destination.Type = s.Type(data.Destination.Type)
	if err := s.ApplyExpr(data.Destination.Value); err != nil {
		return err
	}
	for i := range data.Captures {
		data.Captures[i].Type = s.Type(data.Captures[i].Type)
		if err := s.ApplyExpr(data.Captures[i].Value); err != nil {
			return err
		}
	}
	for i := range data.RemoteOps {
		data.RemoteOps[i].ReceiverType = s.Type(data.RemoteOps[i].ReceiverType)
		if err := s.ApplyExpr(data.RemoteOps[i].Receiver); err != nil {
			return err
		}
		if err := s.ApplyExpr(data.RemoteOps[i].Value); err != nil {
			return err
		}
	}
	data.ReceiverType = s.Type(data.ReceiverType)
	if err := s.ApplyExpr(data.Receiver); err != nil {
		return err
	}
	data.PayloadType = s.Type(data.PayloadType)
	data.ResultType = s.Type(data.ResultType)
	data.HandleType = s.Type(data.HandleType)
	return s.ApplyBlock(data.Body)
}
