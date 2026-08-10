package olcrtcuri

import "testing"

func TestBuildJitsiDatachannel(t *testing.T) {
	got, err := Build(
		"jitsi",
		"datachannel",
		"https://meet.example.org/myroom",
		"d823fa01cb3e0609b67322f7cf984c4ee2e4ce2e294936fc24ef38c9e59f4799",
		"RU / olc free sub",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "olcrtc://jitsi?datachannel@https://meet.example.org/myroom#d823fa01cb3e0609b67322f7cf984c4ee2e4ce2e294936fc24ef38c9e59f4799$RU / olc free sub"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestBuildEscapesDelimitersInRoom(t *testing.T) {
	got, err := Build("wbstream", "vp8channel", "room#frag$x", "aa", "c@mt")
	if err != nil {
		t.Fatal(err)
	}
	want := "olcrtc://wbstream?vp8channel@room%23frag%24x#aa$c%40mt"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestBuildRequiresFields(t *testing.T) {
	if _, err := Build("", "datachannel", "r", "k", ""); err == nil {
		t.Fatal("expected error")
	}
}
