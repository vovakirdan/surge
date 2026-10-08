package mir

import "fmt"

func formatChanSend(send *ChanSendInstr) string {
	kind := "chan_send"
	if send.Resume {
		kind = "chan_send_resume"
	} else if send.TrackConsumed {
		kind += " [consumed=" + formatPlace(send.Consumed) + "]"
	}
	return fmt.Sprintf("%s %s, %s ? bb%d : bb%d", kind,
		formatOperand(&send.Channel), formatOperand(&send.Value), send.ReadyBB, send.PendBB)
}
