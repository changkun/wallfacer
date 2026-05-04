<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue';

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [boolean] }>();

function close() { emit('update:modelValue', false); }
function onOverlayClick(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-overlay')) close();
}
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.modelValue) close();
}
onMounted(() => document.addEventListener('keydown', onKey));
onUnmounted(() => document.removeEventListener('keydown', onKey));
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue"
      class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
      @click="onOverlayClick"
    >
      <div
        class="modal-card"
        style="max-width: 520px; width: 100%; max-height: 85vh; display: flex; flex-direction: column;"
      >
        <div class="p-6" style="display: flex; flex-direction: column; flex: 1; min-height: 0;">
          <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px;">
            <h3 style="font-size: 16px; font-weight: 600; margin: 0;">Keyboard Shortcuts</h3>
            <button
              type="button"
              style="background: none; border: none; cursor: pointer; font-size: 20px; color: var(--text-muted); line-height: 1;"
              @click="close"
            >&times;</button>
          </div>

          <div style="flex: 1; min-height: 0; overflow-y: auto;">
            <div style="margin-bottom: 20px;">
              <h4 style="font-weight: 600; margin: 0 0 8px 0; font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; color: var(--text-muted);">Global</h4>
              <table style="width: 100%; font-size: 12px; border-collapse: collapse;">
                <tr style="border-bottom: 1px solid var(--border);">
                  <td style="padding: 5px 0; width: 140px;"><kbd>n</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">New task</td>
                </tr>
                <tr style="border-bottom: 1px solid var(--border);">
                  <td style="padding: 5px 0;"><kbd>/</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Focus search</td>
                </tr>
                <tr style="border-bottom: 1px solid var(--border);">
                  <td style="padding: 5px 0;"><kbd>Ctrl</kbd> + <kbd>K</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Command palette</td>
                </tr>
                <tr style="border-bottom: 1px solid var(--border);">
                  <td style="padding: 5px 0;"><kbd>Ctrl</kbd> + <kbd>`</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Toggle terminal</td>
                </tr>
                <tr style="border-bottom: 1px solid var(--border);">
                  <td style="padding: 5px 0;"><kbd>Ctrl</kbd> + <kbd>,</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Open settings</td>
                </tr>
                <tr style="border-bottom: 1px solid var(--border);">
                  <td style="padding: 5px 0;"><kbd>?</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Show this help</td>
                </tr>
                <tr>
                  <td style="padding: 5px 0;"><kbd>Escape</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Close modal / cancel</td>
                </tr>
              </table>
            </div>

            <div style="margin-bottom: 20px;">
              <h4 style="font-weight: 600; margin: 0 0 8px 0; font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; color: var(--text-muted);">New Task Form</h4>
              <table style="width: 100%; font-size: 12px; border-collapse: collapse;">
                <tr style="border-bottom: 1px solid var(--border);">
                  <td style="padding: 5px 0; width: 140px;"><kbd>Ctrl</kbd> + <kbd>Enter</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Save task</td>
                </tr>
                <tr>
                  <td style="padding: 5px 0;"><kbd>Escape</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Cancel</td>
                </tr>
              </table>
            </div>

            <div>
              <h4 style="font-weight: 600; margin: 0 0 8px 0; font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; color: var(--text-muted);">Card Navigation</h4>
              <table style="width: 100%; font-size: 12px; border-collapse: collapse;">
                <tr style="border-bottom: 1px solid var(--border);">
                  <td style="padding: 5px 0; width: 140px;"><kbd>Enter</kbd> / <kbd>Space</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Open task</td>
                </tr>
                <tr style="border-bottom: 1px solid var(--border);">
                  <td style="padding: 5px 0;"><kbd>Arrow keys</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Navigate cards</td>
                </tr>
                <tr style="border-bottom: 1px solid var(--border);">
                  <td style="padding: 5px 0;"><kbd>s</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Start backlog task</td>
                </tr>
                <tr style="border-bottom: 1px solid var(--border);">
                  <td style="padding: 5px 0;"><kbd>d</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Done (waiting task)</td>
                </tr>
                <tr>
                  <td style="padding: 5px 0;"><kbd>Escape</kbd></td>
                  <td style="padding: 5px 0; color: var(--text-muted);">Blur card</td>
                </tr>
              </table>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
