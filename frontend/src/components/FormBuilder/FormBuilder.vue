<script setup>
import { reactive, ref, computed, watch } from 'vue'
import { substituteVariables, previewCommands } from '../../utils/commandParser'

const props = defineProps({
  variables: { type: Array, default: () => [] },
  commands: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  showExecute: { type: Boolean, default: true },
  showReset: { type: Boolean, default: true },
  enableRequired: { type: Boolean, default: true },
  enableSubmit: { type: Boolean, default: true },
})

const emit = defineEmits(['submit', 'preview-change'])

const formRef = ref()
const formState = reactive({})
const previews = ref([])

const rules = computed(() => {
  if (!props.enableRequired) return {}
  const r = {}
  props.variables.forEach((v) => {
    if (v.required) {
      r[v.name] = [{ required: true, message: v.description || `请输入 ${v.name}` }]
    }
  })
  return r
})

const resolveInputProps = (variable) => {
  const base = { placeholder: variable.description || variable.name, allowClear: true }
  switch (variable.type) {
    case 'number':
      return { type: 'number', min: 0, ...base }
    default:
      return base
  }
}

const renderInput = (variable) => {
  const type = variable.type || 'string'
  if (type === 'number') return 'a-input-number'
  if (type === 'boolean') return 'a-switch'
  return 'a-input'
}

const updatePreview = () => {
  const substituted = substituteVariables(props.commands, formState)
  previews.value = previewCommands(substituted)
  emit('preview-change', substituted)
}

watch(
  () => props.commands,
  () => {
    updatePreview()
  },
  { deep: true, immediate: true },
)

watch(
  formState,
  () => {
    updatePreview()
  },
  { deep: true },
)

const handleSubmit = async () => {
  if (!props.enableSubmit) return
  try {
    await formRef.value?.validate()
    emit('submit', { ...formState }, previews.value)
  } catch (err) {
    // 保持静默，让表单校验提示显示
  }
}
</script>

<template>
  <div class="form-builder">
    <a-form
      ref="formRef"
      :model="formState"
      :rules="rules"
      layout="vertical"
      @finish="handleSubmit"
    >
      <a-row :gutter="[16, 0]">
        <a-col
          v-for="variable in variables"
          :key="variable.name"
          :xs="24"
          :sm="12"
          :md="12"
          :lg="8"
        >
          <a-form-item
            :name="variable.name"
            :label="variable.description || variable.name"
            :required="props.enableRequired && !!variable.required"
          >
            <component
              v-if="(variable.type || 'string') === 'boolean'"
              :is="renderInput(variable)"
              v-model:checked="formState[variable.name]"
            />
            <component
              v-else
              :is="renderInput(variable)"
              v-model:value="formState[variable.name]"
              v-bind="resolveInputProps(variable)"
            />
          </a-form-item>
        </a-col>
      </a-row>

      <div class="actions">
        <a-space>
          <a-button
            v-if="props.showExecute"
            type="primary"
            :loading="loading"
            :disabled="!props.enableSubmit"
            @click="handleSubmit"
          >
            执行
          </a-button>
          <a-button v-if="props.showReset" @click="() => formRef?.resetFields()">重置</a-button>
        </a-space>
      </div>
    </a-form>

    <div class="preview">
      <div class="preview-title">命令预览</div>
      <a-empty v-if="!previews.length" description="填写参数后查看命令预览" />
      <a-list
        v-else
        size="small"
        :data-source="previews"
        bordered
        :split="false"
        class="preview-list"
      >
        <template #renderItem="{ item, index }">
          <a-list-item>
            <span class="muted">#{{ index + 1 }}</span>
            <a-typography-paragraph code copyable class="preview-line">
              {{ item }}
            </a-typography-paragraph>
          </a-list-item>
        </template>
      </a-list>
    </div>
  </div>
</template>

<style scoped lang="less">
@import "../../styles/variables.less";

.form-builder {
  display: flex;
  flex-direction: column;
  gap: @space-lg;
}

.actions {
  display: flex;
  justify-content: flex-start;
  margin-top: @space-sm;
}

.preview {
  padding: @space-lg;
  background: @color-surface-alt;
  border-radius: @radius-md;
}

.preview-title {
  font-size: @font-size-h3;
  font-weight: @font-weight-medium;
  margin-bottom: @space-sm;
}

.preview-list :deep(.ant-list-item) {
  padding: @space-sm 0;
}

.preview-line {
  margin: 0 0 0 @space-sm;
}
</style>

