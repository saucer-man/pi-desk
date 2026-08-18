export interface RepositoryTreeNode {
  name: string;
  path: string;
  directory: boolean;
  children: RepositoryTreeNode[];
}

export function formatFileMention(path: string, directory = false): string {
  let normalized = path.replaceAll("\\", "/").replace(/\/+$/, "");
  if (directory) normalized += "/";
  if (!/[\s"]/.test(normalized)) return `@${normalized}`;
  return `@"${normalized.replaceAll('"', '\\"')}"`;
}

export function buildRepositoryTree(paths: string[]): RepositoryTreeNode[] {
  const roots: RepositoryTreeNode[] = [];
  for (const rawPath of paths) {
    const parts = rawPath.replaceAll("\\", "/").split("/").filter(Boolean);
    let level = roots;
    let currentPath = "";
    for (let index = 0; index < parts.length; index += 1) {
      const name = parts[index];
      currentPath = currentPath ? `${currentPath}/${name}` : name;
      const directory = index < parts.length - 1;
      let node = level.find((item) => item.name === name && item.directory === directory);
      if (!node) {
        node = { name, path: currentPath, directory, children: [] };
        level.push(node);
      }
      level = node.children;
    }
  }
  const sortNodes = (nodes: RepositoryTreeNode[]) => {
    nodes.sort((left, right) => Number(right.directory) - Number(left.directory) || left.name.localeCompare(right.name));
    nodes.forEach((node) => sortNodes(node.children));
  };
  sortNodes(roots);
  return roots;
}
