export interface Machine { id: string; name: string; format: 'pgs' | 'dcex'; process: string }
export interface RecipeFile { id: string; name: string; format: 'pgs' | 'dcex' }
export interface Field { key: string; value: string }
export interface Parameter { key: string; label: string; value: string; unit: string; description: string; raw: string[] }
export interface TestItem {
  id: string; order: number; number: string; name: string; alias: string;
  kind: 'measurement' | 'control'; function: string;
  lower: string; upper: string; unit: string; time: string; bias: string;
  description: string; sourceLine: number; fields: Field[]; parameters: Parameter[];
}
export interface RecipeBody {
  machineId: string; sourceName: string; format: 'pgs' | 'dcex'; encoding: string;
  sha256: string; tests: TestItem[]; metadata: Field[]; warnings: string[];
}
