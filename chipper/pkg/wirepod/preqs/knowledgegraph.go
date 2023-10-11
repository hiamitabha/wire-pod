package processreqs

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	"github.com/kercre123/chipper/pkg/logger"
	"github.com/kercre123/chipper/pkg/vars"
	"github.com/kercre123/chipper/pkg/vtt"
	sr "github.com/kercre123/chipper/pkg/wirepod/speechrequest"
	"github.com/pkg/errors"
	"github.com/soundhound/houndify-sdk-go"
)

var HKGclient houndify.Client
var HoundEnable bool = true

func ParseSpokenResponse(serverResponseJSON string) (string, error) {
	result := make(map[string]interface{})
	err := json.Unmarshal([]byte(serverResponseJSON), &result)
	if err != nil {
		logger.Println(err.Error())
		return "", errors.New("failed to decode json")
	}
	if !strings.EqualFold(result["Status"].(string), "OK") {
		return "", errors.New(result["ErrorMessage"].(string))
	}
	if result["NumToReturn"].(float64) < 1 {
		return "", errors.New("no results to return")
	}
	return result["AllResults"].([]interface{})[0].(map[string]interface{})["SpokenResponseLong"].(string), nil
}

func InitKnowledge() {
	if vars.APIConfig.Knowledge.Enable && vars.APIConfig.Knowledge.Provider == "houndify" {
		if vars.APIConfig.Knowledge.ID == "" || vars.APIConfig.Knowledge.Key == "" {
			vars.APIConfig.Knowledge.Enable = false
			logger.Println("Houndify Client Key or ID was empty, not initializing kg client")
		} else {
			HKGclient = houndify.Client{
				ClientID:  vars.APIConfig.Knowledge.ID,
				ClientKey: vars.APIConfig.Knowledge.Key,
			}
			HKGclient.EnableConversationState()
			logger.Println("Initialized Houndify client")
		}
	}
}

var NoResult string = "NoResultCommand"
var NoResultSpoken string

func houndifyKG(req sr.SpeechRequest) string {
	var apiResponse string
	if vars.APIConfig.Knowledge.Enable && vars.APIConfig.Knowledge.Provider == "houndify" {
		logger.Println("Sending request to Houndify...")
		serverResponse := StreamAudioToHoundify(req, HKGclient)
		apiResponse, _ = ParseSpokenResponse(serverResponse)
		logger.Println("Houndify response: " + apiResponse)
	} else {
		apiResponse = "Houndify is not enabled."
		logger.Println("Houndify is not enabled.")
	}
	return apiResponse
}

func constructSubstring(input string, maxLength int) string {
    // Split the input string into sentences using periods as the delimiter.
    sentences := strings.Split(input, ".")

    // Initialize an empty substring and a length counter.
    substring := ""
    currentLength := 0

    // Iterate through the sentences and add them to the substring.
    for _, sentence := range sentences {
        // Trim leading and trailing spaces from the sentence.
        sentence = strings.TrimSpace(sentence)

        // Skip empty sentences.
        if sentence == "" {
            continue
        }

        // Calculate the length of the current sentence.
        sentenceLength := len(sentence)

        // Check if adding the current sentence to the substring exceeds the maxLength.
        if currentLength+sentenceLength+len(substring) <= maxLength {
            // Add the sentence to the substring.
            if substring != "" {
                substring += ". "
            }
            substring += sentence
            currentLength += sentenceLength
        } else {
            // Stop adding sentences if maxLength is reached.
            break
        }
    }
    return substring
}


func togetherRequest(transcribedText string) string {
    // Look at the transcribedText and generate an appropriate prompt
    var prompt string
    if strings.Contains(transcribedText, "story") {
         var storyType string
         if strings.Contains(transcribedText, "fairy") {
             storyType = "a fairy tale"
         } else if strings.Contains(transcribedText, "magic") {
             storyType = "a magical kingdom"
         } else if strings.Contains(transcribedText, "stars") {
             storyType = "stars at night"
         } else {
             storyType = "small children"
         }
         prompt = "You are a friendly robot and your job is to help children. The child wants to go to sleep. Could you narrate a story to the child so that she goes to sleep peacefully? The story must be about " + storyType + ". The story must have at most 1000 letters."
    } else {    
	    prompt = "You are a helpful robot called Vector . You will be given a question asked by a user and you must provide the best answer you can. It may not be punctuated or spelled correctly. Keep the answer concise yet informative. Here is the question: " + "\\" + "\"" + transcribedText + "\\" + "\"" + " , Answer: "
    }
	url := "https://api.together.xyz/inference"
    model := vars.APIConfig.Knowledge.Model
	formData := `{
"model": "` + model + `",
"prompt": "` + prompt + `",
"temperature": 0.7,
"max_tokens": 200,
"top_p": 1
}`
	logger.Println("Making request to Together API...")
    logger.Println("Model is " + model)
    logger.Println("Prompt is " + prompt)
    
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer([]byte(formData)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+vars.APIConfig.Knowledge.Key)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "There was an error making the request to Together API"
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
    var togetherResponse map[string]any
	err = json.Unmarshal(body, &togetherResponse)
	if err != nil {
		return "Together API returned no response."
	}
    output := togetherResponse["output"].(map[string]any)
    choice := output["choices"].([]any)
    for _, val := range choice {
        x := val.(map[string]any)
        textResponse := x["text"].(string)
        apiResponse := strings.TrimSuffix(textResponse, "</s>")
        response := constructSubstring(apiResponse, 1000)  
	    logger.Println("Together response: " + response)
        return response
    }
    // In case text is not present in result from API, return a string saying answer was not found
    return "Answer was not found"
}

func openaiRequest(transcribedText string) string {
	sendString := "You are a helpful robot called " + vars.APIConfig.Knowledge.RobotName + ". You will be given a question asked by a user and you must provide the best answer you can. It may not be punctuated or spelled correctly. Keep the answer concise yet informative. Here is the question: " + "\\" + "\"" + transcribedText + "\\" + "\"" + " , Answer: "
	logger.Println("Making request to OpenAI...")
	url := "https://api.openai.com/v1/completions"
	formData := `{
"model": "text-davinci-003",
"prompt": "` + sendString + `",
"temperature": 0.7,
"max_tokens": 256,
"top_p": 1,
"frequency_penalty": 0.2,
"presence_penalty": 0
}`
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer([]byte(formData)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+vars.APIConfig.Knowledge.Key)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logger.Println(err)
		return "There was an error making the request to OpenAI."
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	type openAIStruct struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int    `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Text         string      `json:"text"`
			Index        int         `json:"index"`
			Logprobs     interface{} `json:"logprobs"`
			FinishReason string      `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	var openAIResponse openAIStruct
	err = json.Unmarshal(body, &openAIResponse)
	if err != nil || len(openAIResponse.Choices) == 0 {
		logger.Println("OpenAI returned no response.")
		return "OpenAI returned no response."
	}
	apiResponse := strings.TrimSpace(openAIResponse.Choices[0].Text)
	logger.Println("OpenAI response: " + apiResponse)
	return apiResponse
}

func openaiKG(speechReq sr.SpeechRequest) string {
	transcribedText, err := sttHandler(speechReq)
	if err != nil {
		return "There was an error."
	}
	return openaiRequest(transcribedText)
}

func togetherKG(speechReq sr.SpeechRequest) string {
	transcribedText, err := sttHandler(speechReq)
	if err != nil {
		return "There was an error."
	}
	return togetherRequest(transcribedText)
}

// Takes a SpeechRequest, figures out knowledgegraph provider, makes request, returns API response
func KgRequest(speechReq sr.SpeechRequest) string {
	if vars.APIConfig.Knowledge.Enable {
		if vars.APIConfig.Knowledge.Provider == "houndify" {
			return houndifyKG(speechReq)
		} else if vars.APIConfig.Knowledge.Provider == "openai" {
			return openaiKG(speechReq)
		} else if vars.APIConfig.Knowledge.Provider == "together" {
			return togetherKG(speechReq)
		}
	}
	return "Knowledge graph is not enabled. This can be enabled in the web interface."
}

func (s *Server) ProcessKnowledgeGraph(req *vtt.KnowledgeGraphRequest) (*vtt.KnowledgeGraphResponse, error) {
	sr.BotNum = sr.BotNum + 1
	InitKnowledge()
	speechReq := sr.ReqToSpeechRequest(req)
	apiResponse := KgRequest(speechReq)
	kg := pb.KnowledgeGraphResponse{
		Session:     req.Session,
		DeviceId:    req.Device,
		CommandType: NoResult,
		SpokenText:  apiResponse,
	}
	sr.BotNum = sr.BotNum - 1
	logger.Println("(KG) Bot " + strconv.Itoa(speechReq.BotNum) + " request served.")
	if err := req.Stream.Send(&kg); err != nil {
		return nil, err
	}
	return nil, nil

}
