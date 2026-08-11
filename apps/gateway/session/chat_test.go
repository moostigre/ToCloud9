package session

import "testing"

func TestShouldForwardGameplayWhisper(t *testing.T) {
	tests := []struct {
		name          string
		lang          uint32
		to            string
		msg           string
		characterName string
		want          bool
	}{
		{name: "multispec self whisper", to: "Admin", msg: "SWPMS SWITCH 2", characterName: "Admin", want: true},
		{name: "multispec self whisper case insensitive", to: "ADMIN", msg: "SWPMS STATUS", characterName: "Admin", want: true},
		{name: "multispec whisper to another player", to: "Other", msg: "SWPMS SWITCH 2", characterName: "Admin"},
		{name: "ordinary self whisper", to: "Admin", msg: "hello", characterName: "Admin"},
		{name: "instance difficulty addon message", lang: ^uint32(0), to: "Admin", msg: "SWPIDIFF\tSTATUS", characterName: "Admin", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldForwardGameplayWhisper(test.lang, test.to, test.msg, test.characterName); got != test.want {
				t.Fatalf("shouldForwardGameplayWhisper() = %v, want %v", got, test.want)
			}
		})
	}
}
