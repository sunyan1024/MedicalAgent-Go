package aihelper

import (
	"GoAI/common/rag"
	"context"
	"fmt"
	"log"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

type ragRetrieveDocumentsFunc func(context.Context, string) ([]*schema.Document, error)

type ragNewQueryFunc func(context.Context, string) (ragRetrieveDocumentsFunc, error)

type ragWorkflowState struct {
	Messages       []*schema.Message
	OriginalQuery  string
	RetrievalQuery string
	Docs           []*schema.Document
	MessagesForLLM []*schema.Message
	Fallback       bool
	FallbackReason string
}

func defaultRAGNewQuery(ctx context.Context, username string) (ragRetrieveDocumentsFunc, error) {
	query, err := rag.NewRAGQuery(ctx, username)
	if err != nil {
		return nil, err
	}
	return query.RetrieveDocuments, nil
}

func (o *AliRAGModel) compileRAGWorkflows(ctx context.Context) error {
	start := time.Now()

	generateWorkflow := compose.NewWorkflow[[]*schema.Message, *schema.Message]()
	addRAGPreparationNodes(generateWorkflow, o)
	generateWorkflow.AddLambdaNode("to_messages", compose.InvokableLambda(func(ctx context.Context, state *ragWorkflowState) ([]*schema.Message, error) {
		return state.MessagesForLLM, nil
	})).AddInput("build_prompt")
	generateWorkflow.AddChatModelNode("generate_answer", o.llm).AddInput("to_messages")
	generateWorkflow.End().AddInput("generate_answer")

	generateRunner, err := generateWorkflow.Compile(ctx, compose.WithGraphName("rag_generate_workflow"))
	if err != nil {
		return fmt.Errorf("compile rag generate workflow failed: %w", err)
	}

	prepareWorkflow := compose.NewWorkflow[[]*schema.Message, *ragWorkflowState]()
	addRAGPreparationNodes(prepareWorkflow, o)
	prepareWorkflow.End().AddInput("build_prompt")

	prepareRunner, err := prepareWorkflow.Compile(ctx, compose.WithGraphName("rag_prepare_workflow"))
	if err != nil {
		return fmt.Errorf("compile rag prepare workflow failed: %w", err)
	}

	o.ragGenerateWorkflow = generateRunner
	o.ragPrepareWorkflow = prepareRunner
	log.Printf("[RAG][Workflow] compile success username=%s elapsed=%s", o.username, time.Since(start))
	return nil
}

func addRAGPreparationNodes[I, O any](workflow *compose.Workflow[I, O], model *AliRAGModel) {
	workflow.AddLambdaNode("prepare_query", compose.InvokableLambda(model.prepareRAGWorkflowQuery)).AddInput(compose.START)
	workflow.AddLambdaNode("rewrite_query", compose.InvokableLambda(model.rewriteRAGWorkflowQuery)).AddInput("prepare_query")
	workflow.AddLambdaNode("retrieve_docs", compose.InvokableLambda(model.retrieveRAGWorkflowDocs)).AddInput("rewrite_query")
	workflow.AddLambdaNode("build_prompt", compose.InvokableLambda(model.buildRAGWorkflowPrompt)).AddInput("retrieve_docs")
}

func (o *AliRAGModel) prepareRAGWorkflowQuery(ctx context.Context, messages []*schema.Message) (*ragWorkflowState, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}
	lastMessage := messages[len(messages)-1]
	if lastMessage == nil {
		return nil, fmt.Errorf("last message is nil")
	}
	query := lastMessage.Content
	log.Printf("[RAG][Workflow] query input username=%s query_len=%d query_preview=%q", o.username, len(query), previewRAGQueryForLog(query))
	return &ragWorkflowState{
		Messages:       copySchemaMessages(messages),
		OriginalQuery:  query,
		RetrievalQuery: query,
	}, nil
}

func (o *AliRAGModel) rewriteRAGWorkflowQuery(ctx context.Context, state *ragWorkflowState) (*ragWorkflowState, error) {
	if state == nil {
		return nil, fmt.Errorf("rag workflow state is nil")
	}
	state.RetrievalQuery = o.rewriteRAGQuery(ctx, state.Messages, state.OriginalQuery)
	log.Printf("[RAG][Workflow] query prepared username=%s query_len=%d retrieval_query_len=%d query_preview=%q retrieval_query_preview=%q", o.username, len(state.OriginalQuery), len(state.RetrievalQuery), previewRAGQueryForLog(state.OriginalQuery), previewRAGQueryForLog(state.RetrievalQuery))
	return state, nil
}

func (o *AliRAGModel) retrieveRAGWorkflowDocs(ctx context.Context, state *ragWorkflowState) (*ragWorkflowState, error) {
	if state == nil {
		return nil, fmt.Errorf("rag workflow state is nil")
	}
	newQuery := o.newRAGQuery
	if newQuery == nil {
		newQuery = defaultRAGNewQuery
	}

	queryStart := time.Now()
	retrieveDocuments, err := newQuery(ctx, o.username)
	if err != nil {
		state.Fallback = true
		state.FallbackReason = "query_init_failed"
		log.Printf("[RAG][Workflow] query init failed username=%s fallback=llm err=%v elapsed=%s", o.username, err, time.Since(queryStart))
		return state, nil
	}

	retrieveStart := time.Now()
	docs, err := retrieveDocuments(ctx, state.RetrievalQuery)
	if err != nil {
		state.Fallback = true
		state.FallbackReason = "retrieve_failed"
		log.Printf("[RAG][Workflow] retrieve failed username=%s fallback=llm err=%v elapsed=%s", o.username, err, time.Since(retrieveStart))
		return state, nil
	}
	state.Docs = docs
	log.Printf("[RAG][Workflow] retrieve success username=%s docs=%d elapsed=%s", o.username, len(docs), time.Since(retrieveStart))
	return state, nil
}

func (o *AliRAGModel) buildRAGWorkflowPrompt(ctx context.Context, state *ragWorkflowState) (*ragWorkflowState, error) {
	if state == nil {
		return nil, fmt.Errorf("rag workflow state is nil")
	}
	if state.Fallback || len(state.Docs) == 0 {
		state.MessagesForLLM = copySchemaMessages(state.Messages)
		log.Printf("[RAG][Workflow] prompt skipped username=%s fallback=%v reason=%s docs=%d", o.username, state.Fallback, state.FallbackReason, len(state.Docs))
		return state, nil
	}

	ragPrompt := rag.BuildRAGPromptWithRetrievalQuery(state.OriginalQuery, state.RetrievalQuery, state.Docs)
	state.MessagesForLLM = replaceLastMessageContent(state.Messages, ragPrompt)
	log.Printf("[RAG][Workflow] prompt built username=%s prompt_len=%d docs=%d", o.username, len(ragPrompt), len(state.Docs))
	return state, nil
}

func copySchemaMessages(messages []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, len(messages))
	copy(out, messages)
	return out
}

func replaceLastMessageContent(messages []*schema.Message, content string) []*schema.Message {
	out := copySchemaMessages(messages)
	out[len(out)-1] = &schema.Message{
		Role:    schema.User,
		Content: content,
	}
	return out
}
