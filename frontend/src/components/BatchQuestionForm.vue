<script setup lang="ts">
import { AlertTriangle, Check, ClipboardList } from "lucide-vue-next";
import { computed, reactive, ref } from "vue";
import { tr } from "../i18n";
import { serializeBatchAskAnswers, type BatchAskAnswer, type BatchAskOption, type BatchAskQuestion } from "../utils/batchAsk";

const props = defineProps<{
  questions: BatchAskQuestion[];
  review: boolean;
}>();

const emit = defineEmits<{
  submit: [value: string];
}>();

const currentTab = ref(0);
const answers = reactive<Record<string, BatchAskAnswer | undefined>>({});
const inputValues = reactive<Record<string, string>>({});

function optionForPrefill(question: BatchAskQuestion, prefill: string): BatchAskOption | undefined {
  return question.options?.find((option) => option.value === prefill || option.label === prefill);
}

function setTextAnswer(question: BatchAskQuestion, text: string, custom = false) {
  inputValues[question.id] = text;
  const normalized = question.type === "editor" ? text : text.trim();
  if (!normalized.trim()) {
    answers[question.id] = undefined;
    return;
  }
  answers[question.id] = {
    id: question.id,
    type: question.type,
    value: normalized,
    label: normalized,
    ...(custom ? { wasCustom: true } : {}),
  };
}

for (const question of props.questions) {
  const prefill = question.prefill ?? "";
  inputValues[question.id] = prefill;
  if (!prefill.trim()) continue;
  if (question.type === "select") {
    const option = optionForPrefill(question, prefill);
    if (option) {
      inputValues[question.id] = "";
      answers[question.id] = { id: question.id, type: question.type, value: option.value, label: option.label };
    }
  } else {
    setTextAnswer(question, prefill);
  }
}

const answeredCount = computed(() => props.questions.filter((question) => answers[question.id]).length);
const allAnswered = computed(() => answeredCount.value === props.questions.length);
const serializedAnswers = computed(() => {
  if (!allAnswered.value) return undefined;
  const ordered = props.questions.map((question) => answers[question.id]).filter((answer): answer is BatchAskAnswer => Boolean(answer));
  return serializeBatchAskAnswers(props.questions, ordered);
});
const canSubmit = computed(() => Boolean(serializedAnswers.value));
const isReviewTab = computed(() => props.review && currentTab.value === props.questions.length);
const currentQuestion = computed(() => props.questions[currentTab.value]);

function selectOption(question: BatchAskQuestion, option: BatchAskOption) {
  answers[question.id] = { id: question.id, type: question.type, value: option.value, label: option.label };
}

function selectConfirmation(question: BatchAskQuestion, value: boolean) {
  answers[question.id] = {
    id: question.id,
    type: question.type,
    value,
    label: value ? tr("extension.yes") : tr("extension.no"),
  };
}

function updateText(question: BatchAskQuestion, event: Event) {
  setTextAnswer(question, (event.target as HTMLInputElement | HTMLTextAreaElement).value);
}

function updateCustomDraft(question: BatchAskQuestion, event: Event) {
  inputValues[question.id] = (event.target as HTMLInputElement).value;
  answers[question.id] = undefined;
}

function commitCustom(question: BatchAskQuestion) {
  setTextAnswer(question, inputValues[question.id] ?? "", true);
}

function answerLabel(question: BatchAskQuestion): string {
  const answer = answers[question.id];
  if (!answer) return tr("extension.batchUnanswered");
  if (typeof answer.value === "boolean") return answer.value ? tr("extension.yes") : tr("extension.no");
  return answer.label || answer.value;
}

function submit() {
  if (serializedAnswers.value) emit("submit", serializedAnswers.value);
}
</script>

<template>
  <div class="batch-question-form">
    <div class="batch-question-progress">
      <span>{{ tr("extension.batchProgress", { done: answeredCount, total: questions.length }) }}</span>
    </div>

    <div class="batch-question-tabs" role="tablist" :aria-label="tr('extension.batchTabs')">
      <button
        v-for="(question, index) in questions"
        :id="`batch-question-tab-${question.id}`"
        :key="question.id"
        type="button"
        role="tab"
        :aria-selected="currentTab === index"
        :aria-controls="`batch-question-panel-${question.id}`"
        :class="{ active: currentTab === index, answered: Boolean(answers[question.id]) }"
        @click="currentTab = index"
      >
        <span class="batch-question-tab-number">{{ index + 1 }}</span>
        <span>{{ question.question }}</span>
        <Check v-if="answers[question.id]" :size="13" />
      </button>
      <button
        v-if="review"
        id="batch-question-tab-review"
        type="button"
        role="tab"
        :aria-selected="isReviewTab"
        aria-controls="batch-question-panel-review"
        class="batch-question-review-tab"
        :class="{ active: isReviewTab }"
        @click="currentTab = questions.length"
      >
        <ClipboardList :size="14" />
        <span>{{ tr("extension.batchReviewTab") }}</span>
      </button>
    </div>

    <section
      v-if="isReviewTab"
      id="batch-question-panel-review"
      class="batch-question-panel batch-question-review"
      role="tabpanel"
      aria-labelledby="batch-question-tab-review"
    >
      <header>
        <ClipboardList :size="17" />
        <div>
          <strong>{{ tr("extension.batchReviewTitle") }}</strong>
          <p>{{ tr("extension.batchReviewHint") }}</p>
        </div>
      </header>
      <div class="batch-question-review-list">
        <div v-for="(question, index) in questions" :key="question.id" class="batch-question-review-item">
          <span class="batch-question-review-number">{{ index + 1 }}</span>
          <div>
            <strong>{{ question.question }}</strong>
            <span :class="{ unanswered: !answers[question.id] }">{{ answerLabel(question) }}</span>
          </div>
        </div>
      </div>
      <p v-if="!allAnswered" class="batch-question-warning" role="status">
        <AlertTriangle :size="14" />
        {{ tr("extension.batchIncomplete") }}
      </p>
      <p v-else-if="!canSubmit" class="batch-question-warning" role="status">
        <AlertTriangle :size="14" />
        {{ tr("extension.batchTooLarge") }}
      </p>
      <button class="text-button primary batch-question-submit" type="button" :disabled="!canSubmit" @click="submit">
        {{ tr("extension.batchSubmit") }}
      </button>
    </section>

    <section
      v-else-if="currentQuestion"
      :id="`batch-question-panel-${currentQuestion.id}`"
      class="batch-question-panel"
      role="tabpanel"
      :aria-labelledby="`batch-question-tab-${currentQuestion.id}`"
    >
      <div class="batch-question-heading">
        <span>{{ tr("extension.batchQuestion", { current: currentTab + 1, total: questions.length }) }}</span>
        <h3>{{ currentQuestion.question }}</h3>
      </div>

      <div v-if="currentQuestion.type === 'select'" class="batch-question-options">
        <button
          v-for="option in currentQuestion.options"
          :key="`${currentQuestion.id}-${option.value}`"
          type="button"
          :class="{ selected: answers[currentQuestion.id]?.value === option.value && !answers[currentQuestion.id]?.wasCustom }"
          @click="selectOption(currentQuestion, option)"
        >
          <span class="batch-question-radio" aria-hidden="true"></span>
          <span>
            <strong>{{ option.label }}</strong>
            <small v-if="option.description">{{ option.description }}</small>
          </span>
        </button>
        <div v-if="currentQuestion.allowOther !== false" class="batch-question-custom">
          <label :for="`batch-question-custom-${currentQuestion.id}`">{{ tr("extension.batchOther") }}</label>
          <div>
            <input
              :id="`batch-question-custom-${currentQuestion.id}`"
              :value="inputValues[currentQuestion.id]"
              :placeholder="currentQuestion.placeholder || tr('extension.batchOtherPlaceholder')"
              maxlength="1048576"
              @input="updateCustomDraft(currentQuestion, $event)"
              @keydown.enter.prevent="commitCustom(currentQuestion)"
            />
            <button class="text-button" type="button" :disabled="!inputValues[currentQuestion.id]?.trim()" @click="commitCustom(currentQuestion)">
              {{ tr("extension.batchUseOther") }}
            </button>
          </div>
        </div>
      </div>

      <div v-else-if="currentQuestion.type === 'confirm'" class="batch-question-confirm">
        <button type="button" :class="{ selected: answers[currentQuestion.id]?.value === true }" @click="selectConfirmation(currentQuestion, true)">{{ tr("extension.yes") }}</button>
        <button type="button" :class="{ selected: answers[currentQuestion.id]?.value === false }" @click="selectConfirmation(currentQuestion, false)">{{ tr("extension.no") }}</button>
      </div>

      <textarea
        v-else-if="currentQuestion.type === 'editor'"
        :value="inputValues[currentQuestion.id]"
        :placeholder="currentQuestion.placeholder"
        maxlength="1048576"
        rows="8"
        autofocus
        @input="updateText(currentQuestion, $event)"
      />
      <input
        v-else
        :value="inputValues[currentQuestion.id]"
        :placeholder="currentQuestion.placeholder"
        maxlength="1048576"
        autofocus
        @input="updateText(currentQuestion, $event)"
      />

      <nav class="batch-question-navigation" :aria-label="tr('extension.batchNavigation')">
        <button v-if="currentTab > 0" class="text-button" type="button" @click="currentTab -= 1">{{ tr("extension.batchPrevious") }}</button>
        <span></span>
        <button v-if="currentTab < questions.length - 1" class="text-button primary" type="button" @click="currentTab += 1">{{ tr("extension.batchNext") }}</button>
        <button v-else-if="review" class="text-button primary" type="button" @click="currentTab = questions.length">{{ tr("extension.batchGoReview") }}</button>
        <button v-else class="text-button primary" type="button" :disabled="!canSubmit" @click="submit">{{ tr("extension.batchSubmit") }}</button>
      </nav>
      <p v-if="allAnswered && !canSubmit" class="batch-question-warning" role="status">
        <AlertTriangle :size="14" />
        {{ tr("extension.batchTooLarge") }}
      </p>
    </section>
  </div>
</template>
