<script setup lang="ts">
// Harness brand marks. Geometry is the real official logomark for each
// harness (sourced from the vendors' own brand assets), normalized to its
// native viewBox. By default every mark renders in currentColor so it sits
// cleanly inline with text (HarnessBadge) and adapts to light/dark. Pass
// `color` to use the brand's own color where it has one (only Claude does;
// the others are officially monochrome and stay theme-adaptive).
import { computed, useId } from 'vue';

const props = withDefaults(
  defineProps<{ harness: string; size?: number | string; color?: boolean }>(),
  { size: 16, color: false },
);

type Mark = { viewBox: string; brand?: string };
const MARKS: Record<string, Mark> = {
  claude: { viewBox: '0 0 24 24', brand: '#D97757' },
  codex: { viewBox: '0 0 2406 2406' },
  cursor: { viewBox: '0 0 24 24' },
  opencode: { viewBox: '0 0 24 24' },
  pi: { viewBox: '0 0 800 800' },
  topos: { viewBox: '0 0 24 24', brand: '#55707a' },
};

// Unique per instance so the OpenAI blossom's <use> refs never collide when
// several codex marks render on the same page.
const petalId = `wf-openai-petal-${useId()}`;

const id = computed(() => (props.harness || '').toLowerCase());
const mark = computed<Mark>(() => MARKS[id.value] ?? { viewBox: '0 0 24 24' });
const px = computed(() => (typeof props.size === 'number' ? `${props.size}px` : props.size));
const fill = computed(() => (props.color && mark.value.brand) ? mark.value.brand : 'currentColor');
</script>

<template>
  <svg
    :width="px"
    :height="px"
    :viewBox="mark.viewBox"
    class="harness-logo"
    role="img"
    :aria-label="`${id} logo`"
    :fill="fill"
  >
    <!-- Claude (Anthropic) -->
    <path v-if="id === 'claude'" d="m4.7144 15.9555 4.7174-2.6471.079-.2307-.079-.1275h-.2307l-.7893-.0486-2.6956-.0729-2.3375-.0971-2.2646-.1214-.5707-.1215-.5343-.7042.0546-.3522.4797-.3218.686.0608 1.5179.1032 2.2767.1578 1.6514.0972 2.4468.255h.3886l.0546-.1579-.1336-.0971-.1032-.0972L6.973 9.8356l-2.55-1.6879-1.3356-.9714-.7225-.4918-.3643-.4614-.1578-1.0078.6557-.7225.8803.0607.2246.0607.8925.686 1.9064 1.4754 2.4893 1.8336.3643.3035.1457-.1032.0182-.0728-.164-.2733-1.3539-2.4467-1.445-2.4893-.6435-1.032-.17-.6194c-.0607-.255-.1032-.4674-.1032-.7285L6.287.1335 6.6997 0l.9957.1336.419.3642.6192 1.4147 1.0018 2.2282 1.5543 3.0296.4553.8985.2429.8318.091.255h.1579v-.1457l.1275-1.706.2368-2.0947.2307-2.6957.0789-.7589.3764-.9107.7468-.4918.5828.2793.4797.686-.0668.4433-.2853 1.8517-.5586 2.9021-.3643 1.9429h.2125l.2429-.2429.9835-1.3053 1.6514-2.0643.7286-.8196.85-.9046.5464-.4311h1.0321l.759 1.1293-.34 1.1657-1.0625 1.3478-.8804 1.1414-1.2628 1.7-.7893 1.36.0729.1093.1882-.0183 2.8535-.607 1.5421-.2794 1.8396-.3157.8318.3886.091.3946-.3278.8075-1.967.4857-2.3072.4614-3.4364.8136-.0425.0304.0486.0607 1.5482.1457.6618.0364h1.621l3.0175.2247.7892.522.4736.6376-.079.4857-1.2142.6193-1.6393-.3886-3.825-.9107-1.3113-.3279h-.1822v.1093l1.0929 1.0686 2.0035 1.8092 2.5075 2.3314.1275.5768-.3218.4554-.34-.0486-2.2039-1.6575-.85-.7468-1.9246-1.621h-.1275v.17l.4432.6496 2.3436 3.5214.1214 1.0807-.17.3521-.6071.2125-.6679-.1214-1.3721-1.9246L14.38 17.959l-1.1414-1.9428-.1397.079-.674 7.2552-.3156.3703-.7286.2793-.6071-.4614-.3218-.7468.3218-1.4753.3886-1.9246.3157-1.53.2853-1.9004.17-.6314-.0121-.0425-.1397.0182-1.4328 1.9672-2.1796 2.9446-1.7243 1.8456-.4128.164-.7164-.3704.0667-.6618.4008-.5889 2.386-3.0357 1.4389-1.882.929-1.0868-.0062-.1579h-.0546l-6.3385 4.1164-1.1293.1457-.4857-.4554.0608-.7467.2307-.2429 1.9064-1.3114Z" />

    <!-- Codex (OpenAI blossom) -->
    <g v-else-if="id === 'codex'">
      <path :id="petalId" d="M1107.3 299.1c-197.999 0-373.9 127.3-435.2 315.3L650 743.5v427.9c0 21.4 11 40.4 29.4 51.4l344.5 198.515V833.3h.1v-27.9L1372.7 604c33.715-19.52 70.44-32.857 108.47-39.828L1447.6 450.3C1361 353.5 1237.1 298.5 1107.3 299.1zm0 117.5-.6.6c79.699 0 156.3 27.5 217.6 78.4-2.5 1.2-7.4 4.3-11 6.1L952.8 709.3c-18.4 10.4-29.4 30-29.4 51.4V1248l-155.1-89.4V755.8c-.1-187.099 151.601-338.9 339-339.2z" />
      <use :href="`#${petalId}`" transform="rotate(60 1203 1203)" />
      <use :href="`#${petalId}`" transform="rotate(120 1203 1203)" />
      <use :href="`#${petalId}`" transform="rotate(180 1203 1203)" />
      <use :href="`#${petalId}`" transform="rotate(240 1203 1203)" />
      <use :href="`#${petalId}`" transform="rotate(300 1203 1203)" />
    </g>

    <!-- Cursor -->
    <path v-else-if="id === 'cursor'" d="M11.503.131 1.891 5.678a.84.84 0 0 0-.42.726v11.188c0 .3.162.575.42.724l9.609 5.55a1 1 0 0 0 .998 0l9.61-5.55a.84.84 0 0 0 .42-.724V6.404a.84.84 0 0 0-.42-.726L12.497.131a1.01 1.01 0 0 0-.996 0M2.657 6.338h18.55c.263 0 .43.287.297.515L12.23 22.918c-.062.107-.229.064-.229-.06V12.335a.59.59 0 0 0-.295-.51l-9.11-5.257c-.109-.063-.064-.23.061-.23" />

    <!-- OpenCode -->
    <path v-else-if="id === 'opencode'" d="M22 24H2V0h20zM17 4.8H7v14.4h10z" />

    <!-- Pi (earendil-works/pi) -->
    <g v-else-if="id === 'pi'" fill-rule="evenodd">
      <path d="M165.29 165.29 H517.36 V400 H400 V517.36 H282.65 V634.72 H165.29 Z M282.65 282.65 V400 H400 V282.65 Z" />
      <path d="M517.36 400 H634.72 V634.72 H517.36 Z" />
    </g>

    <!-- Topos (latere.ai native runtime) — 4-node mesh graph -->
    <g
      v-else-if="id === 'topos'"
      fill="none"
      :stroke="fill"
      stroke-width="1.7"
      stroke-linecap="round"
      stroke-linejoin="round"
    >
      <circle cx="6" cy="7" r="2.2" />
      <circle cx="17" cy="6" r="2.2" />
      <circle cx="18" cy="17" r="2.2" />
      <circle cx="7" cy="18" r="2.2" />
      <path d="M8.1 7.4c2.3 1.3 4.8 1.1 6.9-.5M16.5 8.1c1.4 2 1.8 4.3 1.5 6.7M15.9 17.4c-2.1.9-4.4 1.1-6.7.6M6.8 15.8c-.7-2.2-.8-4.4-.2-6.6M9 9.1l6 6" />
    </g>

    <!-- Fallback: generic agent dot -->
    <circle v-else cx="12" cy="12" r="5" fill="none" stroke="currentColor" stroke-width="1.6" />
  </svg>
</template>

<style scoped>
.harness-logo {
  display: inline-block;
  vertical-align: middle;
  flex: none;
}
</style>
