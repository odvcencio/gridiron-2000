package wire

import "testing"

func TestClassifierPrioritizesSpecificSignals(t *testing.T) {
	classifier, err := NewClassifier("")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		text     string
		category string
		relevant bool
	}{
		{name: "touchdown", text: "A 42-yard touchdown for the rookie", category: "touchdown", relevant: true},
		{name: "inactive beats injury", text: "Smith is inactive because of the injury", category: "inactive", relevant: true},
		{name: "turnover", text: "Intercepted at the goal line", category: "turnover", relevant: true},
		{name: "noise", text: "The pregame playlist is immaculate", category: "noise", relevant: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, classifyErr := classifier.Classify(test.text)
			if classifyErr != nil {
				t.Fatal(classifyErr)
			}
			if got.Category != test.category || got.Relevant != test.relevant {
				t.Fatalf("classify %q = %+v", test.text, got)
			}
		})
	}
}

func TestClassifierFiltersCommunityFeedsButKeepsLeagueTips(t *testing.T) {
	classifier, err := NewClassifier("")
	if err != nil {
		t.Fatal(err)
	}
	feedNoise, err := classifier.ClassifyEvidence("My draft logo is finally done", "community_feed")
	if err != nil {
		t.Fatal(err)
	}
	feedSignal, err := classifier.ClassifyEvidence("Receiver did not practice with a hamstring injury", "community_feed")
	if err != nil {
		t.Fatal(err)
	}
	leagueTip, err := classifier.ClassifyEvidence("Watch the backfield rotation tonight", "community")
	if err != nil {
		t.Fatal(err)
	}
	tradeAdvice, err := classifier.ClassifyEvidence("AMA about draft strategy and trade advice", "community_feed")
	if err != nil {
		t.Fatal(err)
	}
	tradeNews, err := classifier.ClassifyEvidence("Veteran receiver has been traded to Buffalo", "community_feed")
	if err != nil {
		t.Fatal(err)
	}
	historicalDiscussion, err := classifier.ClassifyEvidence("What are we doing with this receiver? He missed games because of injury in the last 3 years", "community_feed")
	if err != nil {
		t.Fatal(err)
	}
	if feedNoise.Relevant || !feedSignal.Relevant || feedSignal.Category != "injury" || !leagueTip.Relevant || leagueTip.Category != "community" || tradeAdvice.Relevant || tradeNews.Category != "transaction" || historicalDiscussion.Relevant || historicalDiscussion.Rule != "CommunityDiscussionNoise" {
		t.Fatalf("noise=%+v signal=%+v tip=%+v trade_advice=%+v trade_news=%+v historical=%+v", feedNoise, feedSignal, leagueTip, tradeAdvice, tradeNews, historicalDiscussion)
	}
}
