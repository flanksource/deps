/**
 * Simple Java script example for deps run
 *
 * This will be automatically compiled and executed by deps run
 */
public class Hello {
    public static void main(String[] args) {
        System.out.println("Hello from Java " + System.getProperty("java.version") + "!");
        System.out.println("Platform: " + System.getProperty("os.name") + " " + System.getProperty("os.arch"));

        if (args.length > 0) {
            System.out.print("Arguments: ");
            for (int i = 0; i < args.length; i++) {
                System.out.print(args[i]);
                if (i < args.length - 1) {
                    System.out.print(" ");
                }
            }
            System.out.println();
        }
    }
}
