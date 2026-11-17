import SwiftUI

/// The Ask tab: the conversation that is not tied to one work item.
struct AskView: View {
  var body: some View {
    ChatView(workItem: "")
      .toolbar {
        ToolbarItem(placement: .topBarTrailing) { AccountButton() }
      }
  }
}

/// A conversation with Clanker, global or scoped to one work item.
struct ChatView: View {
  @Environment(AppModel.self) private var app
  @State private var model: ChatModel
  @FocusState private var isComposing: Bool

  init(workItem: String) {
    _model = State(initialValue: ChatModel(workItem: workItem))
  }

  var body: some View {
    ScrollViewReader { proxy in
      ScrollView {
        LazyVStack(alignment: .leading, spacing: 12) {
          ChatIntro(workItem: model.workItem)
          ForEach(model.entries) { entry in
            ChatBubble(entry: entry) { app.open($0) }
              .id(entry.id)
          }
          if model.isSending {
            Label("Clanker reads the records", systemImage: "ellipsis.bubble")
              .font(.callout)
              .foregroundStyle(.secondary)
              .id("sending")
          }
          if let error = model.error {
            ErrorNotice(message: error)
              .padding(12)
              .background(.fill.tertiary, in: RoundedRectangle(cornerRadius: 12))
          }
          SuggestionList(suggestions: model.suggestions) { question in
            model.draft = question
            Task { await model.send(app: app) }
          }
          .disabled(model.isSending)
        }
        .padding()
      }
      .scrollDismissesKeyboard(.interactively)
      .onChange(of: model.entries.last?.id) { _, last in
        guard let last else { return }
        withAnimation { proxy.scrollTo(last, anchor: .bottom) }
      }
    }
    .safeAreaInset(edge: .bottom) { composer }
    .navigationTitle(model.workItem.isEmpty ? "Ask Clanker" : "Ask about \(model.workItem)")
    .navigationBarTitleDisplayMode(.inline)
    .task(id: app.generation) { await model.load(app: app) }
  }

  private var composer: some View {
    HStack(alignment: .bottom, spacing: 8) {
      TextField("Ask a question", text: $model.draft, axis: .vertical)
        .lineLimit(1...4)
        .textFieldStyle(.roundedBorder)
        .focused($isComposing)
        .accessibilityIdentifier("chat-input")
      Button {
        isComposing = false
        Task { await model.send(app: app) }
      } label: {
        Image(systemName: "arrow.up.circle.fill")
          .font(.title)
      }
      .disabled(!model.canSend)
      .accessibilityLabel("Send question")
    }
    .padding(.horizontal)
    .padding(.vertical, 8)
    .background(.bar)
  }
}

private struct ChatIntro: View {
  let workItem: String

  var body: some View {
    VStack(alignment: .leading, spacing: 6) {
      Label(
        workItem.isEmpty
          ? "Answers come from the records of the control plane."
          : "Answers are about \(workItem) and come from its records.",
        systemImage: "sparkles")
      Text(
        "A model writes the answers. Tap a source under an answer to check it. The model "
          + "cannot approve or apply anything.")
    }
    .font(.caption)
    .foregroundStyle(.secondary)
    .accessibilityElement(children: .combine)
  }
}

private struct ChatBubble: View {
  let entry: ChatEntry
  let open: (Citation) -> Void

  var body: some View {
    VStack(alignment: entry.isAssistant ? .leading : .trailing, spacing: 6) {
      Text(entry.isAssistant ? "Clanker · AI-written" : entry.user)
        .font(.caption)
        .foregroundStyle(.secondary)
      Text(entry.body)
        .font(.callout)
        .textSelection(.enabled)
        .padding(10)
        .background(
          entry.isAssistant ? AnyShapeStyle(.fill.tertiary) : AnyShapeStyle(.tint.opacity(0.18)),
          in: RoundedRectangle(cornerRadius: 14))
      if !entry.citations.isEmpty {
        CitationList(citations: entry.citations, open: open)
      }
    }
    .frame(maxWidth: .infinity, alignment: entry.isAssistant ? .leading : .trailing)
    .accessibilityElement(children: .contain)
  }
}

private struct CitationList: View {
  let citations: [Citation]
  let open: (Citation) -> Void

  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      Text("Sources")
        .font(.caption2)
        .foregroundStyle(.secondary)
      ForEach(citations, id: \.self) { citation in
        if citation.type == "work_item" || citation.type == "grant" {
          Button {
            open(citation)
          } label: {
            Label(citation.label, systemImage: symbol(citation))
              .font(.caption)
              .multilineTextAlignment(.leading)
          }
          .buttonStyle(.bordered)
          .buttonBorderShape(.capsule)
          .controlSize(.small)
          .accessibilityHint("Opens the source")
        } else {
          Label(citation.label, systemImage: symbol(citation))
            .font(.caption)
            .foregroundStyle(.secondary)
        }
      }
    }
  }

  private func symbol(_ citation: Citation) -> String {
    switch citation.type {
    case "work_item": "doc.text"
    case "grant": "key"
    default: "link"
    }
  }
}

private struct SuggestionList: View {
  let suggestions: [String]
  let ask: (String) -> Void

  var body: some View {
    VStack(alignment: .leading, spacing: 8) {
      Text("Suggested questions")
        .font(.caption)
        .foregroundStyle(.secondary)
      ForEach(suggestions, id: \.self) { question in
        Button {
          ask(question)
        } label: {
          Text(question)
            .font(.callout)
            .multilineTextAlignment(.leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .buttonStyle(.bordered)
      }
    }
    .padding(.top, 8)
  }
}
