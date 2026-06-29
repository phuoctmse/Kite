package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ActionSpec declares a single permitted action the agent may invoke.
type ActionSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type KiteAgentSpec struct {
	LLMProvider    string          `json:"llmProvider"`           // anthropic | openai | ollama
	LLMEndpoint    string          `json:"llmEndpoint,omitempty"` // optional URL override
	LLMSecret      string          `json:"llmSecret,omitempty"`   // k8s Secret name for API key
	PollInterval   metav1.Duration `json:"pollInterval,omitempty"`
	AllowedActions []ActionSpec    `json:"allowedActions,omitempty"`
	Namespaces     []string        `json:"namespaces,omitempty"` // empty = all namespaces
}

type KiteAgentStatus struct {
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type KiteAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              KiteAgentSpec   `json:"spec,omitempty"`
	Status            KiteAgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type KiteAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KiteAgent `json:"items"`
}
