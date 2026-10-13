const minimum = [22, 12, 0];
const current = process.versions.node.split(".").map((part) => Number(part));

const isSupported =
  current[0] > minimum[0] ||
  (current[0] === minimum[0] &&
    (current[1] > minimum[1] ||
      (current[1] === minimum[1] && current[2] >= minimum[2])));

if (!isSupported) {
  console.error(
    `Node ${process.versions.node} is unsupported. Use Node >=22.12.0 for Doggo release tooling.`,
  );
  process.exit(1);
}
