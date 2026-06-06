package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aitjcize/esp32-photoframe-server/backend/internal/model"
)

type AIGenerationService struct {
	settings *SettingsService
}

func NewAIGenerationService(settings *SettingsService) *AIGenerationService {
	return &AIGenerationService{settings: settings}
}

func (s *AIGenerationService) Generate(device *model.Device) (image.Image, error) {
	fmt.Printf("AI Generation: device=%s, provider=%s, model=%s, prompt=%s\n",
		device.Name, device.AIProvider, device.AIModel, device.AIPrompt)

	if device.AIPrompt == "" {
		return nil, fmt.Errorf("AI prompt not configured for device %s", device.Name)
	}

	provider := device.AIProvider
	modelName := device.AIModel

	if provider == "" {
		return nil, fmt.Errorf("AI provider not configured for device %s", device.Name)
	}
	// ComfyUI selects its model inside the workflow file, so no model name needed.
	if modelName == "" && provider != "comfyui" {
		return nil, fmt.Errorf("AI model not configured for device %s", device.Name)
	}

	isPortrait := device.Height > device.Width
	if device.Orientation == "portrait" {
		isPortrait = true
	} else if device.Orientation == "landscape" {
		isPortrait = false
	}

	switch provider {
	case "openai":
		return s.generateOpenAI(device.AIPrompt, modelName, isPortrait)
	case "google":
		return s.generateGemini(device.AIPrompt, modelName, isPortrait, device.Width, device.Height)
	case "comfyui":
		return s.generateComfyUI(device, isPortrait)
	default:
		return nil, fmt.Errorf("unsupported AI provider: %s", provider)
	}
}

func (s *AIGenerationService) generateOpenAI(prompt, modelName string, isPortrait bool) (image.Image, error) {
	apiKey, err := s.settings.Get("openai_api_key")
	if err != nil || apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key not configured")
	}

	isDalle3 := strings.Contains(modelName, "dall-e-3")
	isDalle2 := strings.Contains(modelName, "dall-e-2")

	size := "1024x1024"
	if isDalle3 {
		if isPortrait {
			size = "1024x1792"
		} else {
			size = "1792x1024"
		}
	} else if isDalle2 {
		size = "1024x1024"
	} else {
		// GPT Image models
		if isPortrait {
			size = "1024x1536"
		} else {
			size = "1536x1024"
		}
	}

	body := map[string]interface{}{
		"model":  modelName,
		"prompt": prompt,
		"n":      1,
		"size":   size,
	}

	if isDalle3 {
		body["quality"] = "hd"
		body["style"] = "vivid"
		body["response_format"] = "b64_json"
	} else if isDalle2 {
		body["response_format"] = "b64_json"
	} else {
		body["quality"] = "high"
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	fmt.Printf("OpenAI request: %s\n", string(jsonBody))

	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/images/generations", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	fmt.Printf("OpenAI response status: %d\n", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("OpenAI error response: %s\n", string(respBody))
		return nil, fmt.Errorf("OpenAI API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no image data in OpenAI response")
	}

	var imgData []byte
	if result.Data[0].B64JSON != "" {
		imgData, err = base64.StdEncoding.DecodeString(result.Data[0].B64JSON)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 image: %w", err)
		}
	} else if result.Data[0].URL != "" {
		imgResp, err := client.Get(result.Data[0].URL)
		if err != nil {
			return nil, fmt.Errorf("failed to download image from URL: %w", err)
		}
		defer imgResp.Body.Close()
		imgData, err = io.ReadAll(imgResp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read image data: %w", err)
		}
	} else {
		return nil, fmt.Errorf("no image data in OpenAI response")
	}

	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return img, nil
}

// generateComfyUI runs a local ComfyUI workflow (e.g. Z-Image Turbo) and returns
// the generated image. The workflow graph is loaded from a JSON file in the data
// volume; the prompt text and latent dimensions are injected and every seed is
// randomized before the graph is queued. Node detection is automatic by
// class_type, with optional comfyui_*_node setting overrides when ambiguous.
func (s *AIGenerationService) generateComfyUI(device *model.Device, isPortrait bool) (image.Image, error) {
	host, _ := s.settings.Get("comfyui_host")
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		return nil, fmt.Errorf("ComfyUI host not configured (set 'comfyui_host', e.g. http://host:8188)")
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "http://" + host
	}

	// Workflow source: prefer the DB setting (managed from the WebGUI); fall
	// back to a file in the data volume for backward compatibility.
	var raw []byte
	wfSource := ""
	if wf, _ := s.settings.Get("comfyui_workflow"); strings.TrimSpace(wf) != "" {
		raw = []byte(wf)
		wfSource = "settings"
	} else {
		wfPath, _ := s.settings.Get("comfyui_workflow_path")
		if strings.TrimSpace(wfPath) == "" {
			dataDir := os.Getenv("DATA_DIR")
			if dataDir == "" {
				dataDir = "/data"
			}
			wfPath = filepath.Join(dataDir, "comfyui_workflow.json")
		}
		fileRaw, err := os.ReadFile(wfPath)
		if err != nil {
			return nil, fmt.Errorf("no ComfyUI workflow configured: set one in Settings or provide %s: %w", wfPath, err)
		}
		raw = fileRaw
		wfSource = wfPath
	}
	var graph map[string]interface{}
	if err := json.Unmarshal(raw, &graph); err != nil {
		return nil, fmt.Errorf("invalid ComfyUI workflow JSON (%s): %w", wfSource, err)
	}
	if len(graph) == 0 {
		return nil, fmt.Errorf("ComfyUI workflow (%s) is empty", wfSource)
	}

	// --- Inject prompt ---
	promptNodeID, err := s.resolveComfyNode(graph, "comfyui_prompt_node", []string{"CLIPTextEncode"},
		func(inputs map[string]interface{}) bool {
			_, ok := inputs["text"]
			return ok
		}, "prompt (CLIPTextEncode)")
	if err != nil {
		return nil, err
	}
	if inputs := comfyInputs(graph, promptNodeID); inputs != nil {
		inputs["text"] = device.AIPrompt
	}

	// --- Inject dimensions ---
	w, h := comfyDimensions(device, isPortrait)
	latentNodeID, err := s.resolveComfyNode(graph, "comfyui_latent_node",
		[]string{"EmptySD3LatentImage", "EmptyLatentImage", "EmptyLatent"},
		func(inputs map[string]interface{}) bool {
			_, okW := inputs["width"]
			_, okH := inputs["height"]
			return okW && okH
		}, "latent size (EmptySD3LatentImage/EmptyLatentImage)")
	if err != nil {
		return nil, err
	}
	if inputs := comfyInputs(graph, latentNodeID); inputs != nil {
		inputs["width"] = w
		inputs["height"] = h
	}

	// --- Randomize every seed so each generation differs ---
	for _, node := range graph {
		inputs := nodeInputs(node)
		if inputs == nil {
			continue
		}
		for _, key := range []string{"seed", "noise_seed"} {
			if _, ok := inputs[key]; ok {
				inputs[key] = rand.Int63n(1 << 53)
			}
		}
	}

	fmt.Printf("ComfyUI Generation: device=%s, host=%s, workflow=%s, %dx%d, prompt=%q\n",
		device.Name, host, wfSource, w, h, device.AIPrompt)

	// --- Queue the prompt ---
	client := &http.Client{Timeout: 30 * time.Second}
	queueBody, err := json.Marshal(map[string]interface{}{"prompt": graph})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ComfyUI prompt: %w", err)
	}
	resp, err := client.Post(host+"/prompt", "application/json", bytes.NewReader(queueBody))
	if err != nil {
		return nil, fmt.Errorf("ComfyUI /prompt request failed: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ComfyUI /prompt error %d: %s", resp.StatusCode, string(body))
	}
	var queued struct {
		PromptID   string                 `json:"prompt_id"`
		NodeErrors map[string]interface{} `json:"node_errors"`
	}
	if err := json.Unmarshal(body, &queued); err != nil {
		return nil, fmt.Errorf("failed to parse ComfyUI /prompt response: %w", err)
	}
	if len(queued.NodeErrors) > 0 {
		ne, _ := json.Marshal(queued.NodeErrors)
		return nil, fmt.Errorf("ComfyUI rejected workflow: %s", string(ne))
	}
	if queued.PromptID == "" {
		return nil, fmt.Errorf("ComfyUI did not return a prompt_id")
	}

	// --- Poll history for completion (generation can take a while) ---
	deadline := time.Now().Add(5 * time.Minute)
	var imgInfo struct{ filename, subfolder, imgType string }
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		hResp, err := client.Get(host + "/history/" + queued.PromptID)
		if err != nil {
			continue
		}
		hBody, _ := io.ReadAll(hResp.Body)
		hResp.Body.Close()
		if hResp.StatusCode != http.StatusOK {
			continue
		}

		var history map[string]struct {
			Status struct {
				StatusStr string `json:"status_str"`
				Completed bool   `json:"completed"`
			} `json:"status"`
			Outputs map[string]struct {
				Images []struct {
					Filename  string `json:"filename"`
					Subfolder string `json:"subfolder"`
					Type      string `json:"type"`
				} `json:"images"`
			} `json:"outputs"`
		}
		if err := json.Unmarshal(hBody, &history); err != nil {
			continue
		}
		entry, ok := history[queued.PromptID]
		if !ok {
			continue
		}
		if entry.Status.StatusStr == "error" {
			return nil, fmt.Errorf("ComfyUI generation failed (status: error)")
		}
		for _, out := range entry.Outputs {
			if len(out.Images) > 0 {
				imgInfo.filename = out.Images[0].Filename
				imgInfo.subfolder = out.Images[0].Subfolder
				imgInfo.imgType = out.Images[0].Type
				break
			}
		}
		if imgInfo.filename != "" {
			break
		}
	}
	if imgInfo.filename == "" {
		return nil, fmt.Errorf("ComfyUI did not produce an image within the time limit")
	}

	// --- Download the rendered image ---
	viewURL := fmt.Sprintf("%s/view?filename=%s&subfolder=%s&type=%s", host,
		url.QueryEscape(imgInfo.filename), url.QueryEscape(imgInfo.subfolder),
		url.QueryEscape(imgInfo.imgType))
	imgResp, err := client.Get(viewURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download ComfyUI image: %w", err)
	}
	defer imgResp.Body.Close()
	if imgResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ComfyUI /view error %d", imgResp.StatusCode)
	}
	imgData, err := io.ReadAll(imgResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read ComfyUI image: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode ComfyUI image: %w", err)
	}
	return img, nil
}

// comfyInputs returns the mutable "inputs" map of a node by ID, or nil.
func comfyInputs(graph map[string]interface{}, nodeID string) map[string]interface{} {
	node, ok := graph[nodeID]
	if !ok {
		return nil
	}
	return nodeInputs(node)
}

// nodeInputs extracts the "inputs" map from a workflow node value.
func nodeInputs(node interface{}) map[string]interface{} {
	nodeMap, ok := node.(map[string]interface{})
	if !ok {
		return nil
	}
	inputs, ok := nodeMap["inputs"].(map[string]interface{})
	if !ok {
		return nil
	}
	return inputs
}

// resolveComfyNode finds the workflow node to patch. Priority: an explicit
// setting override (overrideKey -> node id); otherwise auto-detect by matching
// class_type against classTypes and requiring the node's inputs to satisfy
// hasInputs. Returns a descriptive error (naming the override setting) when zero
// or multiple candidates match, so the user knows which node id to configure.
func (s *AIGenerationService) resolveComfyNode(graph map[string]interface{}, overrideKey string,
	classTypes []string, hasInputs func(map[string]interface{}) bool, label string) (string, error) {
	if override, _ := s.settings.Get(overrideKey); strings.TrimSpace(override) != "" {
		id := strings.TrimSpace(override)
		if _, ok := graph[id]; !ok {
			return "", fmt.Errorf("%s: configured node id %q (%s) not found in workflow", label, id, overrideKey)
		}
		return id, nil
	}

	var matches []string
	for id, node := range graph {
		nodeMap, ok := node.(map[string]interface{})
		if !ok {
			continue
		}
		ct, _ := nodeMap["class_type"].(string)
		matchClass := false
		for _, want := range classTypes {
			if ct == want {
				matchClass = true
				break
			}
		}
		if !matchClass {
			continue
		}
		if hasInputs != nil {
			if inputs, ok := nodeMap["inputs"].(map[string]interface{}); !ok || !hasInputs(inputs) {
				continue
			}
		}
		matches = append(matches, id)
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("could not auto-detect %s node in workflow; set '%s' to the node id", label, overrideKey)
	default:
		return "", fmt.Errorf("found %d candidate %s nodes (%s); set '%s' to the node id to disambiguate",
			len(matches), label, strings.Join(matches, ", "), overrideKey)
	}
}

// comfyDimensions derives generation dimensions from the panel aspect ratio,
// scaled so the long side is ~1024 and both sides are multiples of 64.
func comfyDimensions(device *model.Device, isPortrait bool) (int, int) {
	pw, ph := device.Width, device.Height
	if pw <= 0 || ph <= 0 {
		pw, ph = 1, 1
	}
	long := 1024.0
	short := long * math.Min(float64(pw), float64(ph)) / math.Max(float64(pw), float64(ph))
	roundShort := (int(math.Round(short)) / 64) * 64
	if roundShort < 256 {
		roundShort = 256
	}
	if isPortrait {
		return roundShort, int(long)
	}
	return int(long), roundShort
}

func (s *AIGenerationService) generateGemini(prompt, modelName string, isPortrait bool, width, height int) (image.Image, error) {
	apiKey, err := s.settings.Get("google_api_key")
	if err != nil || apiKey == "" {
		return nil, fmt.Errorf("Google API key not configured")
	}

	aspectRatio := "4:3"
	if isPortrait {
		aspectRatio = "3:4"
	}

	imageConfig := map[string]interface{}{
		"aspectRatio": aspectRatio,
	}

	if strings.Contains(modelName, "gemini-3") {
		maxDim := width
		if height > maxDim {
			maxDim = height
		}
		if maxDim > 2048 {
			imageConfig["imageSize"] = "4K"
		} else if maxDim > 1024 {
			imageConfig["imageSize"] = "2K"
		} else {
			imageConfig["imageSize"] = "1K"
		}
	}

	body := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"responseModalities": []string{"Image"},
			"imageConfig":        imageConfig,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", modelName, apiKey)

	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Gemini API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gemini API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData struct {
						Data     string `json:"data"`
						MimeType string `json:"mimeType"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("no image data in Gemini response")
	}

	b64Data := result.Candidates[0].Content.Parts[0].InlineData.Data
	if b64Data == "" {
		return nil, fmt.Errorf("empty image data in Gemini response")
	}

	imgData, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64 image: %w", err)
	}

	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return img, nil
}
