export interface DocEntry {
  slug: string;
  title: string;
}

export const docIndex: DocEntry[] = [
  { slug: 'getting-started', title: 'Getting Started' },
  { slug: 'autonomy-spectrum', title: 'The Autonomy Spectrum' },
  { slug: 'exploring-ideas', title: 'Exploring Ideas' },
  { slug: 'designing-specs', title: 'Designing Specs' },
  { slug: 'board-and-tasks', title: 'Board & Tasks' },
  { slug: 'automation', title: 'Automation Pipeline' },
  { slug: 'oversight-and-analytics', title: 'Oversight & Analytics' },
  { slug: 'workspaces', title: 'Workspaces & Git' },
  { slug: 'refinement-and-ideation', title: 'Refinement & Ideation' },
  { slug: 'configuration', title: 'Configuration & Customization' },
  { slug: 'circuit-breakers', title: 'Circuit Breakers' },
  { slug: 'usage', title: 'Usage Guide' },
  { slug: 'agents-and-flows', title: 'Agents & Flows' },
];
