#!/usr/bin/env tsx
/**
 * Simple TypeScript script example for deps run
 * Requires tsx or ts-node: npm install -g tsx
 */

import * as os from 'os';

function main(): number {
    console.log(`Hello from Node.js ${process.version} with TypeScript!`);
    console.log(`Platform: ${os.type()} ${os.arch()}`);

    const args: string[] = process.argv.slice(2);
    if (args.length > 0) {
        console.log(`Arguments: ${args.join(' ')}`);
    }

    return 0;
}

process.exit(main());
